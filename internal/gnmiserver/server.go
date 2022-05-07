/*
Copyright 2021 NDD.

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

package gnmiserver

import (
	"context"
	"crypto/tls"
	"net"
	"strconv"
	"strings"

	"github.com/openconfig/gnmi/match"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/pkg/errors"
	pkgmetav1 "github.com/yndd/ndd-core/apis/pkg/meta/v1"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/ndd-target-runtime/internal/cache"
	"github.com/yndd/ndd-target-runtime/internal/targetchannel"
	"golang.org/x/sync/semaphore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	// defaults
	defaultMaxSubscriptions = 64
	defaultMaxGetRPC        = 1024
	certDir                 = "/tmp/k8s-gnmi-server/serving-certs/"
)

// Option can be used to manipulate Options.
type Option func(GnmiServer)

// WithLogger specifies how the Reconciler should log messages.
func WithLogger(log logging.Logger) Option {
	return func(s GnmiServer) {
		s.WithLogger(log)
	}
}

func WithCache(c cache.Cache) Option {
	return func(s GnmiServer) {
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
	return func(s GnmiServer) {
		s.WithTargetChannel(t)
	}
}

type GnmiServer interface {
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

type GnmiServerImpl struct {
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
	m *match.Match // only used for statecache for now -> TBD if we need to make this more
	// gnmi calls
	subscribeRPCsem *semaphore.Weighted
	unaryRPCsem     *semaphore.Weighted
	// logging and parsing
	log logging.Logger

	// context
	ctx context.Context
}

func New(opts ...Option) GnmiServer {
	s := &GnmiServerImpl{
		m: match.New(),
		cfg: &config{
			address:    ":" + strconv.Itoa(pkgmetav1.GnmiServerPort),
			skipVerify: true,
			inSecure:   true,
		},
	}

	for _, opt := range opts {
		opt(s)
	}

	s.ctx = context.Background()

	return s
}

func (s *GnmiServerImpl) WithLogger(log logging.Logger) {
	s.log = log
}

//func (s *GnmiServerImpl) WithEventChannels(e map[string]chan event.GenericEvent) {
//	s.eventChannels = e
//}

func (s *GnmiServerImpl) WithTargetChannel(t chan targetchannel.TargetMsg) {
	s.targetChannel = t
}

func (s *GnmiServerImpl) WithCache(c cache.Cache) {
	s.cache = c
}

func (s *GnmiServerImpl) Start() error {
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
func (s *GnmiServerImpl) run() error {
	s.subscribeRPCsem = semaphore.NewWeighted(defaultMaxSubscriptions)
	s.unaryRPCsem = semaphore.NewWeighted(defaultMaxGetRPC)
	log := s.log.WithValues("grpcServerAddress", s.cfg.address)
	log.Debug("grpc server start...")

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
	gnmi.RegisterGNMIServer(grpcServer, s)
	// attach the gRPC service to the server
	//resourcepb.RegisterResourceServer(grpcServer, s)

	// start the server
	log.Debug("grpc server serve...")
	if err := grpcServer.Serve(l); err != nil {
		s.log.Debug("Errors", "error", err)
		return errors.Wrap(err, "cannot serve grpc server")
	}
	return nil
}

func (s *GnmiServerImpl) serverOpts() ([]grpc.ServerOption, error) {
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
