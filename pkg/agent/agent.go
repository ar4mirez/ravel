package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/valyentdev/ravel/internal/agent/instance"
	"github.com/valyentdev/ravel/internal/agent/instance/state"
	"github.com/valyentdev/ravel/internal/agent/reservations"
	"github.com/valyentdev/ravel/internal/agent/store"
	"github.com/valyentdev/ravel/internal/cluster"
	"github.com/valyentdev/ravel/internal/clustering"
	"github.com/valyentdev/ravel/pkg/core"
	"github.com/valyentdev/ravel/pkg/core/config"
	"github.com/valyentdev/ravel/pkg/runtimes"
	"github.com/valyentdev/ravel/pkg/runtimes/container"
)

type Agent struct {
	node             *clustering.Node
	nodeId           string
	reservations     *reservations.ReservationService
	config           config.AgentConfig
	store            *store.Store
	containerRuntime runtimes.Runtime
	nc               *nats.Conn
	cluster          *cluster.ClusterState
	lock             sync.RWMutex
	instances        map[string]*instance.Manager
}

var _ core.Agent = (*Agent)(nil)

func New(config config.RavelConfig) (*Agent, error) {
	slog.Info("Initializing agent", "node_id", config.NodeId, "address", config.Agent.Address)
	store, err := store.NewStore()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize state: %w", err)
	}

	slog.Info("Initializing container runtime")
	containerRuntime, err := container.NewRuntime(config.Agent)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize container runtime: %w", err)
	}

	natsOptions := []nats.Option{}
	if config.Nats.CredFile != "" {
		natsOptions = append(natsOptions, nats.UserCredentials(config.Nats.CredFile, config.Nats.CredFile))
	}

	slog.Info("Initializing nats")
	nc, err := nats.Connect(config.Nats.Url, natsOptions...)
	if err != nil {
		return nil, err
	}

	reservations := reservations.NewReservationService(store, config.Agent.Resources)

	if err := reservations.Init(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize reservation service: %w", err)
	}

	cs, err := cluster.Connect(config.Corrosion)
	if err != nil {
		return nil, err
	}
	node := clustering.NewNode(cs, core.Node{
		Id:            config.NodeId,
		Address:       config.Agent.Address,
		Region:        config.Agent.Region,
		HeartbeatedAt: time.Now(),
	})

	if err := node.Start(); err != nil {
		return nil, fmt.Errorf("failed to start node: %w", err)
	}

	instances, err := store.ListInstances(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to list instances: %w", err)
	}

	managers := map[string]*instance.Manager{}
	slog.Info("Recovering instances")
	for _, i := range instances {
		lastEvent, err := store.GetLastInstanceEvent(context.Background(), i.Id)
		if err != nil {
			return nil, fmt.Errorf("failed to get last event for instance %s: %w", i.Id, err)
		}

		reservation, err := reservations.GetReservation(context.Background(), i.MachineId)
		if err != nil {
			slog.Error("failed to get reservation", "instanceId", i.Id, "machineId", i.MachineId, "error", err, "reservationId", i.MachineId)
			continue
		}

		i.LocalIPV4 = reservation.LocalIPV4Subnet.LocalConfig().MachineIP.String()

		state := state.NewInstanceState(store, i, &lastEvent, config.NodeId, cs)
		manager := instance.NewInstanceManager(state, containerRuntime, reservation)
		manager.Recover()
		managers[i.Id] = manager
	}

	agent := &Agent{
		node:             node,
		nodeId:           config.NodeId,
		nc:               nc,
		reservations:     reservations,
		config:           config.Agent,
		store:            store,
		containerRuntime: containerRuntime,
		cluster:          cs,
		instances:        managers,
	}
	return agent, nil
}

func (d *Agent) Start(ctx context.Context) error {
	if err := d.node.Start(); err != nil {
		return err
	}

	if err := d.startPlacementHandler(); err != nil {
		return err
	}

	go d.reservations.StartGarbageCollection(ctx)

	return nil
}

func (d *Agent) Stop() error {
	return d.store.Close()
}
