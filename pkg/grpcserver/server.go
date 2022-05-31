/*
Copyright 2021 NDDO.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package grpcserver

import (
	"context"
	"crypto/tls"
	"net"
	"strings"
	"sync"

	//"github.com/openconfig/gnmi/match"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/pkg/errors"

	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/target/internal/cache"
	"github.com/yndd/target/internal/targetchannel"
	"golang.org/x/sync/semaphore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	// defaults
	defaultMaxSubscriptions = 64
	defaultMaxGetRPC        = 1024
	certDir                 = "/tmp/k8s-grpc-server/serving-certs/"
)

// Option can be used to manipulate Options.
type Option func(GrpcServer)

func WithHealth(b bool) Option {
	return func(s GrpcServer) {
		s.WithHealthService(b)
	}
}

func WithGnmi(b bool) Option {
	return func(s GrpcServer) {
		s.WithGnmiService(b)
	}
}

// WithLogger specifies how the Reconciler should log messages.
func WithLogger(log logging.Logger) Option {
	return func(s GrpcServer) {
		s.WithLogger(log)
	}
}

func WithCache(c cache.Cache) Option {
	return func(s GrpcServer) {
		s.WithCache(c)
	}
}

/*
func WithEventChannels(e map[string]chan event.GenericEvent) Option {
	return func(s GnmiServer) {
		s.WithEventChannels(e)
	}
}
*/

func WithTargetChannel(t chan targetchannel.TargetMsg) Option {
	return func(s GrpcServer) {
		s.WithTargetChannel(t)
	}
}

type GrpcServer interface {
	WithHealthService(bool)
	WithGnmiService(bool)
	WithLogger(log logging.Logger)
	WithCache(c cache.Cache)
	//WithEventChannels(e map[string]chan event.GenericEvent)
	WithTargetChannel(t chan targetchannel.TargetMsg)
	Start() error
}

type config struct {
	// Address
	address string
	// Generic
	//maxSubscriptions int64
	//maxUnaryRPC      int64
	// TLS
	inSecure   bool
	skipVerify bool
	//caFile     string
	//certFile   string
	//keyFile    string
	// observability
	//enableMetrics bool
	//debug         bool
}

type GrpcServerImpl struct {
	// capabilities
	health bool
	gnmi   bool

	gnmi.UnimplementedGNMIServer
	healthgrpc.UnimplementedHealthServer

	cfg *config

	// kubernetes
	eventChannels map[string]chan event.GenericEvent
	// target
	targetChannel chan targetchannel.TargetMsg

	// schema
	cache cache.Cache
	//stateCache  *cache.Cache
	//m *match.Match // only used for statecache for now -> TBD if we need to make this more
	// gnmi calls
	subscribeRPCsem *semaphore.Weighted
	unaryRPCsem     *semaphore.Weighted
	// health: statusMap stores the serving status of the services this Server monitors.
	mu        sync.RWMutex
	statusMap map[string]healthpb.HealthCheckResponse_ServingStatus
	updates   map[string]map[healthgrpc.Health_WatchServer]chan healthpb.HealthCheckResponse_ServingStatus
	// logging and parsing
	log logging.Logger

	// context
	ctx context.Context
}

func New(address string, opts ...Option) GrpcServer {
	s := &GrpcServerImpl{
		//m: match.New(),
		statusMap: map[string]healthpb.HealthCheckResponse_ServingStatus{"": healthpb.HealthCheckResponse_SERVING},
		updates:   make(map[string]map[healthgrpc.Health_WatchServer]chan healthpb.HealthCheckResponse_ServingStatus),
		cfg: &config{
			address: address,
			//skipVerify: true,
			//inSecure:   true,
		},
	}

	for _, opt := range opts {
		opt(s)
	}

	s.ctx = context.Background()

	return s
}

func (s *GrpcServerImpl) WithHealthService(b bool) {
	s.health = b
}

func (s *GrpcServerImpl) WithGnmiService(b bool) {
	s.gnmi = b
}

func (s *GrpcServerImpl) WithLogger(log logging.Logger) {
	s.log = log
}

//func (s *GrpcServerImpl) WithEventChannels(e map[string]chan event.GenericEvent) {
//	s.eventChannels = e
//}

func (s *GrpcServerImpl) WithTargetChannel(t chan targetchannel.TargetMsg) {
	s.targetChannel = t
}

func (s *GrpcServerImpl) WithCache(c cache.Cache) {
	s.cache = c
}

func (s *GrpcServerImpl) Start() error {
	log := s.log.WithValues("grpcServerAddress", s.cfg.address)
	log.Debug("grpc server run...")
	errChannel := make(chan error)
	go func() {
		if err := s.run(); err != nil {
			errChannel <- errors.Wrap(err, "cannot start grpc server")
		}
		errChannel <- nil
	}()
	return nil
}

// run GRPC Server
func (s *GrpcServerImpl) run() error {
	log := s.log.WithValues("grpcServerAddress", s.cfg.address)
	log.Debug("grpc server start...")

	// when gnmi is enabled initialize the semaphore
	if s.gnmi {
		s.subscribeRPCsem = semaphore.NewWeighted(defaultMaxSubscriptions)
		s.unaryRPCsem = semaphore.NewWeighted(defaultMaxGetRPC)
	}

	// create a listener on a specific address:port
	l, err := net.Listen("tcp", s.cfg.address)
	if err != nil {
		return errors.Wrap(err, "cannot listen")
	}

	// get server options for certificates
	opts, err := s.serverOpts()
	if err != nil {
		return err
	}

	// create a gRPC server object
	grpcServer := grpc.NewServer(opts...)

	// attach the gnmi service to the grpc server
	if s.gnmi {
		gnmi.RegisterGNMIServer(grpcServer, s)
		log.Debug("grpc server with gnmi...")
	}
	if s.health {
		healthgrpc.RegisterHealthServer(grpcServer, s)
		log.Debug("grpc server with health...")
	}

	// start the server
	log.Debug("grpc server serve...")
	if err := grpcServer.Serve(l); err != nil {
		s.log.Debug("Errors", "error", err)
		return errors.Wrap(err, "cannot serve grpc server")
	}
	return nil
}

func (s *GrpcServerImpl) serverOpts() ([]grpc.ServerOption, error) {
	opts := make([]grpc.ServerOption, 0)
	tlscfg, err := loadTLSCredentials()
	if err != nil {
		return nil, err
	}
	opts = append(opts, grpc.Creds(credentials.NewTLS(tlscfg)))
	return opts, nil

}

func loadTLSCredentials() (*tls.Config, error) {
	// Load server's certificate and private key
	certFile := strings.Join([]string{certDir, "tls.crt"}, "/")
	keyFile := strings.Join([]string{certDir, "tls.key"}, "/")
	serverCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}

	// Create the credentials and return it
	return &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.NoClientCert,
	}, nil
}
