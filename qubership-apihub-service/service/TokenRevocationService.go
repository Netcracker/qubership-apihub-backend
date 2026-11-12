package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/cache"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/buraksezer/olric"
	"github.com/shaj13/go-guardian/v2/auth/claims"
	log "github.com/sirupsen/logrus"
)

type TokenRevocationService interface {
	RevokeUserTokens(ctx context.Context, userId string) error
	IsTokenRevoked(ctx context.Context, userId string, tokenCreationTimestamp int64) (bool, error)
}

func NewTokenRevocationService(provider cache.OlricProvider, cacheTTLSec int) TokenRevocationService {
	tokenRevocationService := &tokenRevocationServiceImpl{
		olricProvider: provider,
		cacheTTL:      time.Duration(cacheTTLSec) * time.Second,
		ready:         make(chan struct{}),
	}

	utils.SafeAsync(func() {
		tokenRevocationService.initWhenOlricReady()
	})
	utils.SafeAsync(func() {
		tokenRevocationService.reinitOnReconnect()
	})
	return tokenRevocationService
}

type tokenRevocationServiceImpl struct {
	// TODO: need to sync the cache to DB periodically and read on startup if no Olric cluster
	olricProvider cache.OlricProvider

	mu                        sync.RWMutex
	userTokenRevocationsCache *olric.DMap

	readyOnce sync.Once
	cacheTTL  time.Duration
	ready     chan struct{}
}

func (l *tokenRevocationServiceImpl) initWhenOlricReady() {
	dmap, err := l.olricProvider.Get().NewDMap("UserTokenRevocations")
	if err != nil {
		log.Errorf("Failed to creare dmap UserTokenRevocations: %s", err.Error())
		log.Infof("Failed to init TokenRevocationService, going to retry")
		time.Sleep(time.Second * 5)
		l.initWhenOlricReady()
		return
	}

	l.mu.Lock()
	l.userTokenRevocationsCache = dmap
	l.mu.Unlock()

	l.readyOnce.Do(func() { close(l.ready) })
	log.Infof("TokenRevocationService is ready")
}

// reinitOnReconnect re-creates the UserTokenRevocations dmap every time the
// underlying Olric node is rebuilt after a network partition (see
// cache.OlricProvider.WaitForReconnect). The old dmap handle belongs to the
// replaced node and would otherwise keep failing forever after a reconnect.
func (l *tokenRevocationServiceImpl) reinitOnReconnect() {
	for {
		if err := l.olricProvider.WaitForReconnect(context.Background()); err != nil {
			return
		}
		log.Infof("TokenRevocationService: olric node reconnected, re-creating dmap UserTokenRevocations")
		l.initWhenOlricReady()
	}
}

func (l *tokenRevocationServiceImpl) dmapCache() *olric.DMap {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.userTokenRevocationsCache
}

func (l *tokenRevocationServiceImpl) RevokeUserTokens(ctx context.Context, userId string) error {
	select {
	case <-l.ready:
	case <-ctx.Done():
		return ctx.Err()
	}

	// We need to take into account that go-guardian adds leeway when issuing tokens to avoid a situation where freshly issued tokens are considered revoked
	currentTimestamp := time.Now().Add(-claims.DefaultLeeway).Unix()
	dmapCache := l.dmapCache()
	if dmapCache == nil {
		return fmt.Errorf("failed to revoke tokens for user %s since the UserTokenRevocations cache is not initialized", userId)
	}
	if err := dmapCache.PutEx(userId, currentTimestamp, l.cacheTTL); err != nil {
		return err
	}
	return nil
}

// IsTokenRevoked reports whether the token was revoked. A non-nil error means the check could not be
// performed because the caller's context is done; it is returned rather than reported as a revocation
// so that a timed-out request is not presented to the user as an authentication failure. Cache
// failures keep failing closed.
func (l *tokenRevocationServiceImpl) IsTokenRevoked(ctx context.Context, userId string, tokenCreationTimestamp int64) (bool, error) {
	dmapCache := l.dmapCache()
	if dmapCache == nil {
		return false, nil
	}

	if err := ctx.Err(); err != nil {
		return false, err
	}

	val, err := dmapCache.Get(userId)
	if err != nil {
		if errors.Is(err, olric.ErrKeyNotFound) {
			return false, nil
		}
		log.Errorf("Error getting revocation timestamp: %v", err)
		return true, nil
	}
	revocationTimestamp, _ := val.(int64)

	return tokenCreationTimestamp <= revocationTimestamp, nil
}
