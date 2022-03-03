// Copyright 2022 Chaos Mesh Authors.
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

package tasks

import (
	"github.com/chaos-mesh/chaos-mesh/pkg/chaoserr"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"sync"
)

var ErrNotFoundSysPID = chaoserr.NotFound("SysPID")
var ErrNotPodPID = errors.New("pid is not PodPID")
var ErrPodProcessMapNotInit = errors.New("PodProcessMap not init")

type PodID struct {
	podID UID
}

func (p PodID) ToID() string {
	return p.podID
}

type ChaosOnPOD interface {
	Injectable
	Recoverable
}

type PodProcessMap struct {
	m      map[PodID]SysPID
	rwLock sync.RWMutex
}

func NewPodProcessMap() PodProcessMap {
	return PodProcessMap{
		m:      make(map[PodID]SysPID),
		rwLock: sync.RWMutex{},
	}
}

func (p *PodProcessMap) Read(podPID PodID) (SysPID, error) {
	p.rwLock.RLock()
	defer p.rwLock.RUnlock()
	sysPID, ok := p.m[podPID]
	if !ok {
		return SysPID(0), ErrNotFoundSysPID
	}
	return sysPID, nil
}

func (p *PodProcessMap) Write(podPID PodID, sysPID SysPID) {
	p.rwLock.Lock()
	defer p.rwLock.Unlock()
	p.m[podPID] = sysPID
}

// PodHandler implements injecting & recovering on a kubernetes POD.
type PodHandler struct {
	PodProcessMap *PodProcessMap
	Main          ChaosOnPOD
	Logger        logr.Logger
}

func NewPodHandler(podProcessMap *PodProcessMap, main ChaosOnPOD, logger logr.Logger) PodHandler {
	return PodHandler{
		PodProcessMap: podProcessMap,
		Main:          main,
		Logger:        logr.New(logger.GetSink()),
	}
}

func (p *PodHandler) Inject(pid PID) error {
	podPID, ok := pid.(PodID)
	if !ok {
		return ErrNotPodPID
	}
	if p.PodProcessMap == nil {
		return ErrPodProcessMapNotInit
	}

	sysPID, err := p.PodProcessMap.Read(podPID)
	if err != nil {
		return err
	}

	err = p.Main.Inject(sysPID)
	return err
}

func (p *PodHandler) Recover(pid PID) error {
	podPID, ok := pid.(PodID)
	if !ok {
		return ErrNotPodPID
	}
	if p.PodProcessMap == nil {
		return ErrPodProcessMapNotInit
	}

	sysPID, err := p.PodProcessMap.Read(podPID)
	if err != nil {
		return err
	}

	err = p.Main.Recover(sysPID)
	return err
}
