package healthhandler

import (
	"context"
	"sync"

	"github.com/yndd/ndd-runtime/pkg/logging"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

type Options struct {
	Logger logging.Logger
}

type SubServer interface {
	Check(ctx context.Context, in *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error)
	Watch(in *healthpb.HealthCheckRequest, stream healthpb.Health_WatchServer) error
}

func New(o *Options) SubServer {
	s := &subServer{
		log:       o.Logger,
		mu:        sync.RWMutex{},
		statusMap: map[string]healthpb.HealthCheckResponse_ServingStatus{"": healthpb.HealthCheckResponse_SERVING},
		updates:   make(map[string]map[healthpb.Health_WatchServer]chan healthpb.HealthCheckResponse_ServingStatus),
	}
	return s
}

type subServer struct {
	log       logging.Logger
	mu        sync.RWMutex
	statusMap map[string]healthpb.HealthCheckResponse_ServingStatus
	updates   map[string]map[healthpb.Health_WatchServer]chan healthpb.HealthCheckResponse_ServingStatus
}
