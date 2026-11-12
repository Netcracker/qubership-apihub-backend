package cache

import (
	"context"
	"encoding/gob"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"

	sysconfig "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/config"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/buraksezer/olric"
	discovery "github.com/buraksezer/olric-cloud-plugin/lib"
	"github.com/buraksezer/olric/config"
	"github.com/hashicorp/memberlist"
	log "github.com/sirupsen/logrus"
)

// OlricProvider gives access to the local Olric cluster node.
//
// Olric (as of its current release, including its continuation under
// github.com/olric-data/olric) never retries a peer that memberlist has marked
// dead: the only Join attempt happens once, at initial bootstrap, and the
// library's own Rejoin helper is never called anywhere internally. So once a
// network error causes a peer to be marked "left", nothing reconnects it on its
// own, even after connectivity is restored. This provider works around that gap
// by rebuilding the local node from scratch (see watchForIsolation) whenever it
// has been isolated for too long, which forces discovery and Join to run again.
type OlricProvider interface {
	Get() *olric.Olric
	GetBindAddr() string
	// WaitForReconnect blocks until the local Olric node has been torn down and
	// rejoined to the cluster after a network partition, or until ctx is done.
	// Callers that hold long-lived DTopic/DMap handles obtained from a previous
	// Get() must re-create them after it returns nil: those handles belong to
	// the replaced node and stop working once it is shut down.
	WaitForReconnect(ctx context.Context) error
}

const (
	olricBindAddr = "0.0.0.0"
	// nodeEventChannelSize buffers memberlist join/leave notifications so a burst
	// of membership churn cannot block memberlist's own event delegate.
	nodeEventChannelSize = 64
	// reconnectJoinTimeout bounds how long a reconnect attempt waits for the
	// rebuilt node to finish joining before it is treated as failed and retried
	// after another grace period. It matches the BootstrapTimeout used for the
	// initial Join in "lan" mode.
	reconnectJoinTimeout = 60 * time.Second
	// olricShutdownTimeout bounds how long the replaced node is given to close
	// its connections after a successful reconnect.
	olricShutdownTimeout = 10 * time.Second
)

type olricProviderImpl struct {
	startupWg sync.WaitGroup // signals readiness only for the very first start

	mu         sync.RWMutex
	cfg        *config.Config
	olricC     *olric.Olric
	nodeEvents chan memberlist.NodeEvent
	changed    chan struct{} // closed and replaced every time olricC is swapped

	olricConfig          sysconfig.OlricConfig
	reconnectGracePeriod time.Duration
}

func NewOlricProvider(olricConfig sysconfig.OlricConfig) (OlricProvider, error) {
	prov := &olricProviderImpl{
		olricConfig:          olricConfig,
		reconnectGracePeriod: time.Duration(olricConfig.ReconnectGracePeriodSec) * time.Second,
		changed:              make(chan struct{}),
	}

	gob.Register(map[string]interface{}{})

	cfg, nodeEvents, err := buildConfigWithEvents(olricConfig)
	if err != nil {
		return nil, err
	}

	prov.startupWg.Add(1)
	cfg.Started = prov.startupWg.Done

	olricC, err := olric.New(cfg)
	if err != nil {
		return nil, err
	}

	prov.cfg = cfg
	prov.olricC = olricC
	prov.nodeEvents = nodeEvents

	go func() {
		if err := olricC.Start(); err != nil {
			log.Panicf("Olric cache node cannot be started. Error: %s", err.Error())
		}
	}()

	utils.SafeAsync(prov.watchForIsolation)

	return prov, nil
}

func (op *olricProviderImpl) Get() *olric.Olric {
	op.startupWg.Wait()
	op.mu.RLock()
	defer op.mu.RUnlock()
	return op.olricC
}

func (op *olricProviderImpl) GetBindAddr() string {
	op.startupWg.Wait()
	op.mu.RLock()
	defer op.mu.RUnlock()
	return op.cfg.BindAddr
}

func (op *olricProviderImpl) WaitForReconnect(ctx context.Context) error {
	op.mu.RLock()
	changed := op.changed
	op.mu.RUnlock()

	select {
	case <-changed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (op *olricProviderImpl) currentNodeEvents() chan memberlist.NodeEvent {
	op.mu.RLock()
	defer op.mu.RUnlock()
	return op.nodeEvents
}

// watchForIsolation runs for the lifetime of the provider. It rebuilds the
// local Olric node whenever no peer has rejoined within reconnectGracePeriod of
// the last NodeLeave, which is the only way to recover from a network partition
// given Olric's lack of a built-in rejoin (see the OlricProvider doc comment).
func (op *olricProviderImpl) watchForIsolation() {
	nodeEvents := op.currentNodeEvents()

	isolationTimer := time.NewTimer(op.reconnectGracePeriod)
	if !isolationTimer.Stop() {
		<-isolationTimer.C
	}
	defer isolationTimer.Stop()

	for {
		select {
		case event := <-nodeEvents:
			switch event.Event {
			case memberlist.NodeLeave:
				log.Warnf("OlricProvider: peer %s left the cluster; will force this node to rejoin in %s if no peer returns before then", event.Node.Name, op.reconnectGracePeriod)
				isolationTimer.Reset(op.reconnectGracePeriod)
			case memberlist.NodeJoin:
				isolationTimer.Stop()
			}
		case <-isolationTimer.C:
			log.Warnf("OlricProvider: no peer rejoined within %s of the last network partition, forcing this node to rejoin the cluster", op.reconnectGracePeriod)
			if op.reconnect() {
				nodeEvents = op.currentNodeEvents()
			} else {
				isolationTimer.Reset(op.reconnectGracePeriod)
			}
		}
	}
}

// reconnect tears down the current Olric node and starts a fresh one in its
// place, so it goes through discovery and Join again. It returns false if the
// new node could not be started; the caller retries after another grace
// period instead of treating the failure as fatal, since this runs long after
// startup wiring has already succeeded once.
func (op *olricProviderImpl) reconnect() bool {
	cfg, nodeEvents, err := buildConfigWithEvents(op.olricConfig)
	if err != nil {
		log.Errorf("OlricProvider: failed to rebuild the olric config while reconnecting: %s", err.Error())
		return false
	}

	started := make(chan struct{})
	cfg.Started = func() { close(started) }

	newC, err := olric.New(cfg)
	if err != nil {
		log.Errorf("OlricProvider: failed to create a new olric node while reconnecting: %s", err.Error())
		return false
	}

	startErr := make(chan error, 1)
	utils.SafeAsync(func() { startErr <- newC.Start() })

	select {
	case <-started:
	case err := <-startErr:
		if err != nil {
			log.Errorf("OlricProvider: new olric node failed to start while reconnecting: %s", err.Error())
			return false
		}
	case <-time.After(reconnectJoinTimeout):
		log.Errorf("OlricProvider: timed out after %s waiting for the new olric node to join the cluster", reconnectJoinTimeout)
		return false
	}

	op.mu.Lock()
	oldC := op.olricC
	oldChanged := op.changed
	op.olricC = newC
	op.cfg = cfg
	op.nodeEvents = nodeEvents
	op.changed = make(chan struct{})
	op.mu.Unlock()
	close(oldChanged)

	log.Infof("OlricProvider: node rejoined the cluster (bindAddr=%s bindPort=%d)", cfg.BindAddr, cfg.BindPort)

	utils.SafeAsync(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), olricShutdownTimeout)
		defer cancel()
		if err := oldC.Shutdown(shutdownCtx); err != nil {
			log.Warnf("OlricProvider: failed to shut down the replaced olric node cleanly: %s", err.Error())
		}
	})

	return true
}

// buildConfigWithEvents builds an Olric config and wires a fresh memberlist
// node-event channel into it, so the caller can observe NodeJoin/NodeLeave for
// whichever node is ultimately started from the returned config.
func buildConfigWithEvents(olricConfig sysconfig.OlricConfig) (*config.Config, chan memberlist.NodeEvent, error) {
	cfg, err := getConfig(olricConfig)
	if err != nil {
		return nil, nil, err
	}

	nodeEvents := make(chan memberlist.NodeEvent, nodeEventChannelSize)
	cfg.MemberlistConfig.Events = &memberlist.ChannelEventDelegate{Ch: nodeEvents}

	return cfg, nodeEvents, nil
}

func getConfig(olricConfig sysconfig.OlricConfig) (*config.Config, error) {
	mode := olricConfig.DiscoveryMode
	switch mode {
	case "lan":
		log.Info("Olric run in cloud mode")
		cfg := config.New(mode)

		cfg.LogLevel = "WARN"
		cfg.LogVerbosity = 2

		namespace := olricConfig.Namespace
		if namespace == "" {
			return nil, fmt.Errorf("namespace is not set")
		}

		cloudDiscovery := &discovery.CloudDiscovery{}
		cfg.ServiceDiscovery = map[string]interface{}{
			"plugin":   cloudDiscovery,
			"provider": "k8s",
			"args":     fmt.Sprintf("namespace=%s label_selector=\"%s\"", namespace, "olric-cluster=apihub"), // select pods with label "olric-cluster=apihub"
		}

		// TODO: try to get from replica set via kube client
		replicaCount := olricConfig.ReplicaCount
		log.Infof("replicaCount is set to %d", replicaCount)

		cfg.PartitionCount = uint64(replicaCount * 4)
		cfg.ReplicaCount = replicaCount

		cfg.MemberCountQuorum = int32(replicaCount)
		cfg.BootstrapTimeout = 60 * time.Second
		cfg.MaxJoinAttempts = 60

		return cfg, nil
	case "local":
		log.Info("Olric run in local mode")
		cfg := config.New(mode)

		cfg.LogLevel = "WARN"
		cfg.LogVerbosity = 2

		cfg.BindAddr = olricBindAddr
		cfg.BindPort = getLocalPort(olricConfig.BindPort)
		cfg.MemberlistConfig.BindAddr = olricBindAddr
		cfg.MemberlistConfig.BindPort = getLocalMemberlistPort(olricConfig.MemberlistPort)
		cfg.PartitionCount = 5

		return cfg, nil
	default:
		log.Warnf("Unknown olric discovery mode %s. Will use default \"local\" mode", mode)
		return config.New("local"), nil
	}
}

func getLocalPort(configuredPort int) int {
	//try configured port first
	if isPortFree(olricBindAddr, configuredPort) {
		return configuredPort
	}
	//and if fails, then random
	return getLocalRandomFreePort()
}
func getLocalMemberlistPort(configuredPort int) int {
	//try configured port first
	if isPortFree(olricBindAddr, configuredPort) {
		return configuredPort
	}
	//and if fails, then random
	return getLocalRandomFreePort()
}

func getLocalRandomFreePort() int {
	for {
		port := rand.Intn(48127) + 1024
		if isPortFree(olricBindAddr, port) {
			return port
		}
		if isPortFree(olricBindAddr, port) {
			return port
		}
	}
}

func isPortFree(address string, port int) bool {
	ln, err := net.Listen("tcp", address+":"+strconv.Itoa(port))

	if err != nil {
		return false
	}

	_ = ln.Close()
	return true
}
