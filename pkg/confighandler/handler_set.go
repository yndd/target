package confighandler

import (
	"context"
	"time"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/yndd/ndd-runtime/pkg/meta"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// errors
	errTargetNotFoundInCache = "could not find target in cache"
)

func (s *subServer) Set(ctx context.Context, p *gnmi.Path, upd *gnmi.Update) (*gnmi.SetResponse, error) {
	cacheNsTargetName := meta.NamespacedName(p.GetTarget()).GetPrefixNamespacedName(p.GetOrigin())

	ce, err := s.cache.GetEntry(cacheNsTargetName)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, errTargetNotFoundInCache)
	}

	if err := ce.SetSystemCacheStatus(true); err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	return &gnmi.SetResponse{
		Response: []*gnmi.UpdateResult{
			{
				Timestamp: time.Now().UnixNano(),
			},
		}}, nil
}

func (s *subServer) Delete(ctx context.Context, p *gnmi.Path, del *gnmi.Path) (*gnmi.SetResponse, error) {
	cacheNsTargetName := meta.NamespacedName(p.GetTarget()).GetPrefixNamespacedName(p.GetOrigin())

	ce, err := s.cache.GetEntry(cacheNsTargetName)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, errTargetNotFoundInCache)
	}

	if err := ce.SetSystemCacheStatus(true); err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	return &gnmi.SetResponse{
		Response: []*gnmi.UpdateResult{
			{
				Timestamp: time.Now().UnixNano(),
			},
		}}, nil
}
