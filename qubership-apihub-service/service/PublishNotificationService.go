package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"github.com/google/uuid"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/cache"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"

	"github.com/buraksezer/olric"
	log "github.com/sirupsen/logrus"
)

func NewPublishNotificationService(op cache.OlricProvider) PublishNotificationService {
	trh := publishNotificationServiceImpl{
		op:    op,
		ready: make(chan struct{}),
	}
	utils.SafeAsync(func() {
		trh.initVersionPublishedDTopic()
	})
	utils.SafeAsync(func() {
		trh.reinitOnReconnect()
	})
	return &trh
}

type PublishNotificationService interface {
	SendNotification(ctx context.Context, packageId string, version string, revision int) error
}

type publishNotificationServiceImpl struct {
	op cache.OlricProvider

	mu                    sync.RWMutex
	versionPublishedTopic *olric.DTopic

	readyOnce sync.Once
	ready     chan struct{}
}

const VersionPublishedTopicName = "version-published"

func (t *publishNotificationServiceImpl) SendNotification(ctx context.Context, packageId string, version string, revision int) error {
	select {
	case <-t.ready:
	case <-ctx.Done():
		return ctx.Err()
	}

	t.mu.RLock()
	topic := t.versionPublishedTopic
	t.mu.RUnlock()

	if topic == nil {
		return fmt.Errorf("failed to publish message to %s DTopic since it's not initialized", VersionPublishedTopicName)
	}

	msg := view.PublishNotification{
		EventId:   uuid.NewString(),
		PackageId: packageId,
		Version:   version,
		Revision:  revision,
	}

	jsonMsg, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	err = topic.Publish(string(jsonMsg))
	if err != nil {
		log.Errorf("Failed to send 'version published' event: %s", err)
		return err
	}
	return nil
}

func (t *publishNotificationServiceImpl) initVersionPublishedDTopic() {
	var err error
	for attempt := 1; attempt < 4; attempt++ {
		var topic *olric.DTopic
		topic, err = t.op.Get().NewDTopic(VersionPublishedTopicName, 10000, olric.UnorderedDelivery)
		if err != nil {
			log.Errorf("Failed to create DTopic %s (attempt %d): %s", VersionPublishedTopicName, attempt, err.Error())
			time.Sleep(10 * time.Second)
			continue
		}
		t.mu.Lock()
		t.versionPublishedTopic = topic
		t.mu.Unlock()
		break
	}
	t.readyOnce.Do(func() { close(t.ready) })
}

// reinitOnReconnect re-creates the "version-published" DTopic every time the
// underlying Olric node is rebuilt after a network partition (see
// cache.OlricProvider.WaitForReconnect). The old DTopic handle belongs to the
// replaced node, so without this, SendNotification would keep failing forever
// after the first reconnect.
func (t *publishNotificationServiceImpl) reinitOnReconnect() {
	for {
		if err := t.op.WaitForReconnect(context.Background()); err != nil {
			return
		}
		log.Infof("PublishNotificationService: olric node reconnected, re-creating DTopic %s", VersionPublishedTopicName)
		t.initVersionPublishedDTopic()
	}
}
