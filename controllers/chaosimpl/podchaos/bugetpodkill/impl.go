package bugetpodkill

import (
	"context"

	"github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	impltypes "github.com/chaos-mesh/chaos-mesh/controllers/chaosimpl/types"
	"github.com/chaos-mesh/chaos-mesh/controllers/chaosimpl/utils"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ impltypes.ChaosImpl = (*Impl)(nil)

type Impl struct {
	client.Client

	Log logr.Logger

	decoder *utils.ContainerRecordDecoder
}

func (impl *Impl) Apply(ctx context.Context, index int, records []*v1alpha1.Record, obj v1alpha1.InnerObject) (v1alpha1.Phase, error) {
	impl.Log.Info("apply pod budget kill")
	return v1alpha1.Injected, nil
}

func (impl *Impl) Recover(ctx context.Context, index int, records []*v1alpha1.Record, obj v1alpha1.InnerObject) (v1alpha1.Phase, error) {
	impl.Log.Info("recover pod budget kill")
	return v1alpha1.NotInjected, nil
}

func NewImpl(c client.Client, log logr.Logger, decoder *utils.ContainerRecordDecoder) *Impl {
	return &Impl{
		Client:  c,
		Log:     log.WithName("containerkill"),
		decoder: decoder,
	}
}
