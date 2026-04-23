// Package redis ports redis_store.py — same key scheme so Go and Python
// can share Redis during shadow-mode parallel operation.
package redis

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	keyPrefix = "callified:"
	callTTL   = time.Hour
)

// PendingCallInfo mirrors the dict stored by python's set_pending_call().
type PendingCallInfo struct {
	Name          string `json:"name"`
	Phone         string `json:"phone"`
	LeadID        int64  `json:"lead_id"`
	Interest      string `json:"interest"`
	ExotelCallSid string `json:"exotel_call_sid"`
	CampaignID    int64  `json:"campaign_id"`
	TTSProvider   string `json:"tts_provider"`
	TTSVoiceID    string `json:"tts_voice_id"`
	TTSLanguage   string `json:"tts_language"`
}

// Store wraps a Redis client with in-memory fallback (mirrors redis_store.py).
type Store struct {
	rdb    *goredis.Client
	log    *zap.Logger
	mu     sync.RWMutex
	memPending  map[string]PendingCallInfo
	memTakeover map[string]bool
	memWhisper  map[string][]string
}

// New creates a Store. If the Redis URL is unreachable the store silently
// falls back to in-memory maps — same behaviour as python redis_store.py.
func New(redisURL string, log *zap.Logger) *Store {
	s := &Store{
		log:         log,
		memPending:  make(map[string]PendingCallInfo),
		memTakeover: make(map[string]bool),
		memWhisper:  make(map[string][]string),
	}
	opt, err := goredis.ParseURL(redisURL)
	if err != nil {
		log.Warn("redis: invalid URL, using in-memory fallback", zap.Error(err))
		return s
	}
	opt.DialTimeout = 2 * time.Second
	s.rdb = goredis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.rdb.Ping(ctx).Err(); err != nil {
		log.Warn("redis: ping failed, using in-memory fallback", zap.Error(err))
		s.rdb = nil
	}
	return s
}

func key(parts ...string) string { return keyPrefix + strings.Join(parts, ":") }

// ─── Pending Calls ────────────────────────────────────────────────────────────

func (s *Store) SetPendingCall(ctx context.Context, k string, info PendingCallInfo) error {
	data, _ := json.Marshal(info)
	if s.rdb != nil {
		return s.rdb.SetEx(ctx, key("pending", k), data, callTTL).Err()
	}
	s.mu.Lock()
	s.memPending[k] = info
	s.mu.Unlock()
	return nil
}

func (s *Store) GetPendingCall(ctx context.Context, k string) (PendingCallInfo, bool) {
	if s.rdb != nil {
		val, err := s.rdb.Get(ctx, key("pending", k)).Bytes()
		if err != nil {
			return PendingCallInfo{}, false
		}
		var info PendingCallInfo
		if err := json.Unmarshal(val, &info); err != nil {
			return PendingCallInfo{}, false
		}
		return info, true
	}
	s.mu.RLock()
	info, ok := s.memPending[k]
	s.mu.RUnlock()
	return info, ok
}

func (s *Store) DeletePendingCall(ctx context.Context, k string) {
	if s.rdb != nil {
		s.rdb.Del(ctx, key("pending", k))
		return
	}
	s.mu.Lock()
	delete(s.memPending, k)
	s.mu.Unlock()
}

// ─── Takeover ─────────────────────────────────────────────────────────────────

func (s *Store) SetTakeover(ctx context.Context, streamSid string, active bool) error {
	val := "0"
	if active {
		val = "1"
	}
	if s.rdb != nil {
		return s.rdb.SetEx(ctx, key("takeover", streamSid), val, callTTL).Err()
	}
	s.mu.Lock()
	s.memTakeover[streamSid] = active
	s.mu.Unlock()
	return nil
}

func (s *Store) GetTakeover(ctx context.Context, streamSid string) bool {
	if s.rdb != nil {
		val, err := s.rdb.Get(ctx, key("takeover", streamSid)).Result()
		return err == nil && val == "1"
	}
	s.mu.RLock()
	v := s.memTakeover[streamSid]
	s.mu.RUnlock()
	return v
}

func (s *Store) DeleteTakeover(ctx context.Context, streamSid string) {
	if s.rdb != nil {
		s.rdb.Del(ctx, key("takeover", streamSid))
		return
	}
	s.mu.Lock()
	delete(s.memTakeover, streamSid)
	s.mu.Unlock()
}

// ─── Whisper Queue ────────────────────────────────────────────────────────────

func (s *Store) PushWhisper(ctx context.Context, streamSid, message string) error {
	if s.rdb != nil {
		pipe := s.rdb.Pipeline()
		pipe.RPush(ctx, key("whisper", streamSid), message)
		pipe.Expire(ctx, key("whisper", streamSid), callTTL)
		_, err := pipe.Exec(ctx)
		return err
	}
	s.mu.Lock()
	s.memWhisper[streamSid] = append(s.memWhisper[streamSid], message)
	s.mu.Unlock()
	return nil
}

// PopAllWhispers atomically retrieves and clears the whisper queue.
// Mirrors python's redis_store.pop_all_whispers() PIPELINE(LRANGE, DEL).
func (s *Store) PopAllWhispers(ctx context.Context, streamSid string) ([]string, error) {
	if s.rdb != nil {
		k := key("whisper", streamSid)
		var msgs []string
		err := s.rdb.Watch(ctx, func(tx *goredis.Tx) error {
			vals, err := tx.LRange(ctx, k, 0, -1).Result()
			if err != nil {
				return err
			}
			msgs = vals
			_, err = tx.TxPipelined(ctx, func(pipe goredis.Pipeliner) error {
				pipe.Del(ctx, k)
				return nil
			})
			return err
		}, k)
		return msgs, err
	}
	s.mu.Lock()
	msgs := s.memWhisper[streamSid]
	delete(s.memWhisper, streamSid)
	s.mu.Unlock()
	return msgs, nil
}

func (s *Store) DeleteWhispers(ctx context.Context, streamSid string) {
	if s.rdb != nil {
		s.rdb.Del(ctx, key("whisper", streamSid))
		return
	}
	s.mu.Lock()
	delete(s.memWhisper, streamSid)
	s.mu.Unlock()
}

// ─── Cleanup ──────────────────────────────────────────────────────────────────

func (s *Store) CleanupCall(ctx context.Context, streamSid string) {
	s.DeleteTakeover(ctx, streamSid)
	s.DeleteWhispers(ctx, streamSid)
}

// ─── Pub/Sub (SSE fan-out) ────────────────────────────────────────────────────

// Publish sends a message to a Redis channel.
// Silently no-ops when Redis is unavailable.
func (s *Store) Publish(ctx context.Context, channel, message string) {
	if s.rdb == nil {
		return
	}
	s.rdb.Publish(ctx, keyPrefix+channel, message)
}

// Subscribe returns a channel that receives messages published to the given channel.
// The returned channel is closed when ctx is cancelled or Redis is unavailable.
func (s *Store) Subscribe(ctx context.Context, channel string) <-chan string {
	ch := make(chan string, 32)
	if s.rdb == nil {
		close(ch)
		return ch
	}
	sub := s.rdb.Subscribe(ctx, keyPrefix+channel)
	go func() {
		defer close(ch)
		msgCh := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				sub.Close()
				return
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				select {
				case ch <- msg.Payload:
				default:
				}
			}
		}
	}()
	return ch
}

// ─── Live Logs ─────────────────────────────────────────────────────────────────

// GetLiveLogs returns the last n entries from the callified:live-logs Redis list.
// Returns an empty slice when Redis is unavailable.
func (s *Store) GetLiveLogs(ctx context.Context, n int) ([]string, error) {
	if s.rdb == nil {
		return []string{}, nil
	}
	if n <= 0 {
		n = 100
	}
	return s.rdb.LRange(ctx, key("live-logs"), int64(-n), -1).Result()
}
