package sqldb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/upper/db/v4"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-workflows/v3/config"
)

// SessionProxy is a wrapper for upperdb sessions that provides automatic reconnection
// on network failures through a With() method pattern.
type SessionProxy struct {
	// Connection configuration for reconnection
	kubectlConfig kubernetes.Interface
	namespace     string
	dbConfig      config.DBConfig
	username      string
	password      string

	// Current session and state
	sess   db.Session
	mu     sync.RWMutex
	closed bool

	// Retry configuration
	maxRetries    int
	baseDelay     time.Duration
	maxDelay      time.Duration
	retryMultiple float64
}

// SessionProxyConfig contains configuration for creating a SessionProxy
type SessionProxyConfig struct {
	KubectlConfig kubernetes.Interface
	Namespace     string
	DBConfig      config.DBConfig
	Username      string
	Password      string
	MaxRetries    int
	BaseDelay     time.Duration
	MaxDelay      time.Duration
}

// NewSessionProxy creates a new SessionProxy with the given configuration
func NewSessionProxy(ctx context.Context, config SessionProxyConfig) (*SessionProxy, error) {
	proxy := &SessionProxy{
		kubectlConfig: config.KubectlConfig,
		namespace:     config.Namespace,
		dbConfig:      config.DBConfig,
		username:      config.Username,
		password:      config.Password,
		maxRetries:    config.MaxRetries,
		baseDelay:     config.BaseDelay,
		maxDelay:      config.MaxDelay,
		retryMultiple: 2.0,
	}

	if proxy.maxRetries == 0 {
		proxy.maxRetries = 5
	}
	if proxy.baseDelay == 0 {
		proxy.baseDelay = 100 * time.Millisecond
	}
	if proxy.maxDelay == 0 {
		proxy.maxDelay = 30 * time.Second
	}

	if err := proxy.connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to create initial database session: %w", err)
	}

	return proxy, nil
}

// NewSessionProxyFromSession creates a SessionProxy from an existing session with credentials
func NewSessionProxyFromSession(sess db.Session, dbConfig config.DBConfig, username, password string) *SessionProxy {
	return &SessionProxy{
		sess:          sess,
		dbConfig:      dbConfig,
		username:      username,
		password:      password,
		maxRetries:    5,
		baseDelay:     100 * time.Millisecond,
		maxDelay:      30 * time.Second,
		retryMultiple: 2.0,
	}
}

func (sp *SessionProxy) connect(ctx context.Context) error {
	var sess db.Session
	var err error

	if sp.kubectlConfig != nil && sp.namespace != "" {
		// Use Kubernetes secrets for authentication
		sess, err = CreateDBSession(ctx, sp.kubectlConfig, sp.namespace, sp.dbConfig)
	} else if sp.username != "" && sp.password != "" {
		// Use direct credentials
		sess, err = CreateDBSessionWithCreds(sp.dbConfig, sp.username, sp.password)
	} else {
		return fmt.Errorf("insufficient authentication information provided")
	}

	if err != nil {
		return err
	}

	sp.sess = sess
	return nil
}

func (sp *SessionProxy) isNetworkError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	// Common network error patterns
	networkErrors := []string{
		"connection refused",
		"connection reset",
		"connection lost",
		"connection closed",
		"network is unreachable",
		"no route to host",
		"timeout",
		"broken pipe",
		"connection timed out",
		"i/o timeout",
		"eof",
		"connection aborted",
		"connection dropped",
		"server closed the connection",
		"bad connection",
		"invalid connection",
		"connection is not available",
		"connection has been closed",
		"driver: bad connection",
	}

	for _, pattern := range networkErrors {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	// Check for specific error types
	if netErr, ok := err.(net.Error); ok {
		return netErr.Timeout()
	}

	// Check for driver.ErrBadConn
	if err == driver.ErrBadConn {
		return true
	}

	// Check for sql.ErrConnDone
	if err == sql.ErrConnDone {
		return true
	}

	return false
}

// With executes with a db Session
func (sp *SessionProxy) With(ctx context.Context, fn func(db.Session) error) error {
	sp.mu.RLock()
	if sp.closed {
		sp.mu.RUnlock()
		return fmt.Errorf("session proxy is closed")
	}
	sp.mu.RUnlock()

	var lastErr error
	delay := sp.baseDelay

	for attempt := 0; attempt <= sp.maxRetries; attempt++ {
		// Get current session
		sp.mu.RLock()
		sess := sp.sess
		sp.mu.RUnlock()

		if sess == nil {
			sp.mu.RUnlock()
			return fmt.Errorf("no active session")
		}

		// Execute the operation
		err := fn(sess)
		if err == nil {
			return nil
		}

		lastErr = err

		// If it's not a network error, don't retry
		if !sp.isNetworkError(err) {
			return err
		}

		// If this is the last attempt, don't retry
		if attempt == sp.maxRetries {
			break
		}

		// Wait before retrying
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		// Try to reconnect
		sp.mu.Lock()
		if !sp.closed {
			if sp.sess != nil {
				sp.sess.Close() // Close the bad connection
			}
			if reconnectErr := sp.connect(ctx); reconnectErr != nil {
				sp.mu.Unlock()
				// If reconnection fails, continue with the original error
				continue
			}
		}
		sp.mu.Unlock()

		// Exponential backoff
		delay = time.Duration(float64(delay) * sp.retryMultiple)
		if delay > sp.maxDelay {
			delay = sp.maxDelay
		}
	}

	return fmt.Errorf("operation failed after %d retries, last error: %w", sp.maxRetries, lastErr)
}

// Reconnect accepts an error and outputs a new sessions
func (sp *SessionProxy) Reconnect(ctx context.Context, err error) (db.Session, error) {
	if !sp.isNetworkError(err) {
		return nil, err
	}
	sp.sess.Close()
	err = sp.connect(ctx)
	if err != nil {
		return nil, err
	}
	return nil, nil
}

// Session returns the underlying session. Use With() for operations that need reconnection.
// This method is provided for cases where you need direct access to the session,
// but it won't provide automatic reconnection.
func (sp *SessionProxy) Session() db.Session {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.sess
}

// Close closes the session proxy and underlying session
func (sp *SessionProxy) Close() error {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.closed {
		return nil
	}

	sp.closed = true

	if sp.sess != nil {
		return sp.sess.Close()
	}

	return nil
}

// Ping tests the connection with automatic retry
func (sp *SessionProxy) Ping(ctx context.Context) error {
	return sp.With(ctx, func(sess db.Session) error {
		return sess.Ping()
	})
}
