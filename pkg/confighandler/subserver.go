package confighandler

import (
	"context"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/target/internal/cache"
)

type Options struct {
	Logger logging.Logger
	Cache  cache.Cache
}

type SubServer interface {
	Get(ctx context.Context, req *gnmi.GetRequest) (*gnmi.GetResponse, error)
	Set(ctx context.Context, p *gnmi.Path, upd *gnmi.Update) (*gnmi.SetResponse, error)
	Delete(ctx context.Context, p *gnmi.Path, del *gnmi.Path) (*gnmi.SetResponse, error)
}

func New(o *Options) SubServer {
	s := &subServer{
		log:   o.Logger,
		cache: o.Cache,
	}
	return s
}

type subServer struct {
	log   logging.Logger
	cache cache.Cache
}
