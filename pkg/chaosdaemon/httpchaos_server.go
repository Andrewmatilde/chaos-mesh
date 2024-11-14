// Copyright 2021 Chaos Mesh Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package chaosdaemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	"os"
	"sync"

	"github.com/pkg/errors"

	"github.com/chaos-mesh/chaos-mesh/pkg/bpm"
	pb "github.com/chaos-mesh/chaos-mesh/pkg/chaosdaemon/pb"
	"github.com/chaos-mesh/chaos-mesh/pkg/chaosdaemon/tproxyconfig"
)

const (
	tproxyBin = "/usr/local/bin/tproxy"
	pathEnv   = "PATH"
)

type stdioTransport struct {
	uid    string
	locker *sync.Map
	pipes  bpm.Pipes
}

func (t *stdioTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	if _, loaded := t.locker.LoadOrStore(t.uid, true); loaded {
		return &http.Response{
			StatusCode: http.StatusLocked,
			Status:     http.StatusText(http.StatusLocked),
			Body:       io.NopCloser(bytes.NewBufferString("")),
			Request:    req,
		}, nil
	}
	defer t.locker.Delete(t.uid)
	if t.pipes.Stdin == nil {
		return nil, errors.New("fail to get stdin of process")
	}
	if t.pipes.Stdout == nil {
		return nil, errors.New("fail to get stdout of process")
	}

	err = req.Write(t.pipes.Stdin)
	if err != nil {
		return
	}

	resp, err = http.ReadResponse(bufio.NewReader(t.pipes.Stdout), req)
	return
}

func (s *DaemonServer) ApplyHttpChaos(ctx context.Context, in *pb.ApplyHttpChaosRequest) (*pb.ApplyHttpChaosResponse, error) {
	log := s.getLoggerFromContext(ctx)
	log.Info("applying http chaos", "in.InstanceUid", in.InstanceUid)

	if in.InstanceUid == "" {
		log.Info("instance uid is empty, try to get it from background process manager")
		if uid, ok := s.backgroundProcessManager.GetUID(bpm.ProcessPair{Pid: int(in.Instance), CreateTime: in.StartTime}); ok {
			log.Info("get instance uid from background process manager", "uid", uid)
			in.InstanceUid = uid
		}
	}

	if _, ok := s.backgroundProcessManager.GetPipes(in.InstanceUid); !ok {
		log.Info("instance not found, create a new one")
		if in.InstanceUid != "" {
			// chaos daemon may restart, create another tproxy instance

			if err := s.backgroundProcessManager.KillBackgroundProcess(ctx, in.InstanceUid); err != nil {
				// ignore this error
				log.Error(err, "kill background process", "uid", in.InstanceUid)
			}
		}

		// set uid internally
		if err := s.createHttpChaos(ctx, in); err != nil {
			return nil, errors.Wrap(err, "create http chaos")
		}
	}

	resp, err := s.applyHttpChaos(ctx, in)
	if err != nil {
		if killError := s.backgroundProcessManager.KillBackgroundProcess(ctx, in.InstanceUid); killError != nil {
			log.Error(killError, "kill tproxy", "uid", in.InstanceUid)
		}
		return nil, errors.Wrap(err, "apply config")
	}
	return resp, err
}

func (s *DaemonServer) applyHttpChaos(ctx context.Context, in *pb.ApplyHttpChaosRequest) (*pb.ApplyHttpChaosResponse, error) {
	log := s.getLoggerFromContext(ctx)

	pipes, ok := s.backgroundProcessManager.GetPipes(in.InstanceUid)
	if !ok {
		log.Error(errors.Errorf("fail to get process(%s)", in.InstanceUid), "in.InstanceUid", in.InstanceUid)
		return nil, errors.Errorf("fail to get process(%s)", in.InstanceUid)
	}

	transport := &stdioTransport{
		uid:    in.InstanceUid,
		locker: s.tproxyLocker,
		pipes:  pipes,
	}

	var rules []tproxyconfig.PodHttpChaosBaseRule
	err := json.Unmarshal([]byte(in.Rules), &rules)
	if err != nil {
		log.Error(err, "unmarshal rules", "rules", in.Rules)
		return nil, errors.Wrap(err, "unmarshal rules")
	}
	log.Info("the length of actions", "length", len(rules))

	httpChaosSpec := tproxyconfig.Config{
		ProxyPorts: in.ProxyPorts,
		Rules:      rules,
	}

	if len(in.Tls) != 0 {
		httpChaosSpec.TLS = new(tproxyconfig.TLSConfig)
		err = json.Unmarshal([]byte(in.Tls), httpChaosSpec.TLS)
		if err != nil {
			return nil, errors.Wrap(err, "unmarshal tls config")
		}
	}

	config, err := json.Marshal(&httpChaosSpec)
	if err != nil {
		log.Error(err, "marshal config", "config", httpChaosSpec)
		return nil, err
	}

	log.Info("ready to apply", "config", string(config))

	req, err := http.NewRequest(http.MethodPut, "/", bytes.NewReader(config))
	if err != nil {
		log.Error(err, "create http request")
		return nil, errors.Wrap(err, "create http request")
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		log.Error(err, "send http request")
		return nil, errors.Wrap(err, "send http request")
	}

	log.Info("http chaos applied")

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error(err, "read response body")
		return nil, errors.Wrap(err, "read response body")
	}

	log.Info("response", "status", resp.StatusCode, "body", string(body))
	return &pb.ApplyHttpChaosResponse{
		Instance:    int64(in.Instance),
		InstanceUid: in.InstanceUid,
		StartTime:   in.StartTime,
		StatusCode:  int32(resp.StatusCode),
		Error:       string(body),
	}, nil
}

func (s *DaemonServer) createHttpChaos(ctx context.Context, in *pb.ApplyHttpChaosRequest) error {
	pid, err := s.crClient.GetPidFromContainerID(ctx, in.ContainerId)
	if err != nil {
		log.Error(err, "get PID of container", "containerId", in.ContainerId)
		return errors.Wrapf(err, "get PID of container(%s)", in.ContainerId)
	}
	log.Info("get PID of container", "containerId", in.ContainerId, "pid", pid)
	processBuilder := bpm.DefaultProcessBuilder(tproxyBin, "-i", "-vv").
		EnableLocalMnt().
		SetIdentifier(fmt.Sprintf("tproxy-%s", in.ContainerId)).
		SetEnv(pathEnv, os.Getenv(pathEnv))

	log.Info("Check if enter network namespace", "enterNS", in.EnterNS)
	if in.EnterNS {
		processBuilder = processBuilder.SetNS(pid, bpm.PidNS).SetNS(pid, bpm.NetNS)
	}

	cmd := processBuilder.Build(ctx)
	cmd.Stderr = os.Stderr

	log.Info("execute command", "cmd", cmd.String())
	proc, err := s.backgroundProcessManager.StartProcess(ctx, cmd)
	if err != nil {
		log.Error(err, "execute command", "cmd", cmd.String())
		return errors.Wrapf(err, "execute command(%s)", cmd)
	}

	in.Instance = int64(proc.Pair.Pid)
	in.StartTime = proc.Pair.CreateTime
	in.InstanceUid = proc.Uid
	log.Info("create base for http chaos", "in.InstanceUid", in.InstanceUid)
	return nil
}
