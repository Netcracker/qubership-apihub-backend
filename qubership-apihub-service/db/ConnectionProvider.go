package db

import (
	"context"
	"fmt"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"github.com/go-pg/pg/v10"
	log "github.com/sirupsen/logrus"
)

type ConnectionProvider interface {
	GetConnection() *pg.DB
}

type connectionProviderImpl struct {
	creds view.DbCredentials
	db    *pg.DB
}

func NewConnectionProvider(creds *view.DbCredentials) ConnectionProvider {
	return &connectionProviderImpl{creds: *creds}
}

func (c *connectionProviderImpl) GetConnection() *pg.DB {
	if c.db == nil {
		c.db = pg.Connect(&pg.Options{
			Addr:       fmt.Sprintf("%s:%d", c.creds.Host, c.creds.Port),
			User:       c.creds.Username,
			Password:   c.creds.Password,
			Database:   c.creds.Database,
			PoolSize:   50,
			MaxRetries: 5,
		})
		c.db.AddQueryHook(contextErrorHook{})
	}
	//c.db.AddQueryHook(&dbLogger{cpi: c})
	return c.db
}

type contextErrorHook struct{}

func (contextErrorHook) BeforeQuery(ctx context.Context, _ *pg.QueryEvent) (context.Context, error) {
	return ctx, nil
}

// AfterQuery restores the context error when go-pg reports a query that failed because the caller's
// context was cancelled. Depending on which of go-pg's two cancellation paths wins, the raw failure is
// an i/o timeout or Postgres SQLSTATE 57014, neither of which errors.Is can match against the context
// sentinels. The context is only consulted when the query actually failed, so a query that succeeded
// just before the deadline keeps its result.
func (contextErrorHook) AfterQuery(ctx context.Context, event *pg.QueryEvent) error {
	if event.Err == nil {
		return nil
	}
	ctxErr := ctx.Err()
	if ctxErr == nil {
		return nil
	}
	log.Warnf("Query aborted because the request context is done: %v", event.Err)
	return fmt.Errorf("query aborted: %w", ctxErr)
}

//TODO: is this still needed ?
/*
type dbLogger struct {
	cpi *connectionProviderImpl
}

func (d dbLogger) BeforeQuery(ctx context.Context, q *pg.QueryEvent) (context.Context, error) {
	return ctx, nil
}

func (d dbLogger) AfterQuery(ctx context.Context, q *pg.QueryEvent) error {
	if query, _ := q.FormattedQuery(); bytes.Compare(query, []byte("SELECT 1")) != 0 {
		log.Trace(string(query))
	}

	if q.Err != nil && strings.Contains(q.Err.Error(), "Conn is in a bad state") {
		if d.cpi != nil {
			if d.cpi.conn != nil {
				err := d.cpi.conn.Close()
				if err != nil {
					log.Errorf("Failed to close conn for bad state: %s", err)
				}
			}
			if d.cpi.db != nil {
				err := d.cpi.db.Close()
				if err != nil {
					log.Errorf("Failed to close DB for bad state: %s", err)
				}
			}
		}
		d.cpi.db = nil
		d.cpi.conn = nil
	}

	// for dev purposes
	//queryStr, _ := q.FormattedQuery()
	//log.Infof("DB query: %s", queryStr)

	return nil
}*/
