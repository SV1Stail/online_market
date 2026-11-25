package ratelimit

import (
	"context"
	"fmt"
	"time"

	redis "github.com/redis/go-redis/v9"

	"github.com/SV1Stail/online_market/internal/identifier"
)

type Rule struct {
	Limit  int64
	Window time.Duration
}

type Policy struct {
	Name  string
	Rules []Rule
}

type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
}

type RedisLimiter struct {
	client redis.Scripter
	prefix string
}

func NewRedisLimiter(client redis.Scripter, prefix string) *RedisLimiter {
	return &RedisLimiter{client: client, prefix: prefix}
}

func (l *RedisLimiter) Allow(ctx context.Context, subject string, policy Policy) (*Decision, error) {
	if subject == "" || policy.Name == "" || len(policy.Rules) == 0 {
		return nil, fmt.Errorf("rate limit subject and policy are required")
	}
	member, err := identifier.NewUUID()
	if err != nil {
		return nil, fmt.Errorf("generate rate limit member: %w", err)
	}

	keys := make([]string, 0, len(policy.Rules))
	arguments := make([]any, 0, 1+2*len(policy.Rules))
	arguments = append(arguments, member)
	for index, rule := range policy.Rules {
		if rule.Limit <= 0 || rule.Window < time.Millisecond {
			return nil, fmt.Errorf("invalid rate limit rule")
		}
		keys = append(keys, fmt.Sprintf("%s:{%s:%s}:%d", l.prefix, subject, policy.Name, index))
		arguments = append(arguments, rule.Limit, rule.Window.Milliseconds())
	}

	result, err := slidingWindowScript.Run(ctx, l.client, keys, arguments...).Slice()
	if err != nil {
		return nil, fmt.Errorf("execute rate limit script: %w", err)
	}
	if len(result) != 2 {
		return nil, fmt.Errorf("unexpected rate limit result")
	}
	allowed, ok := result[0].(int64)
	if !ok {
		return nil, fmt.Errorf("unexpected rate limit decision")
	}
	retryMilliseconds, ok := result[1].(int64)
	if !ok {
		return nil, fmt.Errorf("unexpected rate limit retry delay")
	}
	return &Decision{
		Allowed:    allowed == 1,
		RetryAfter: time.Duration(retryMilliseconds) * time.Millisecond,
	}, nil
}

var slidingWindowScript = redis.NewScript(`
local redis_time = redis.call('TIME')
local now_us = tonumber(redis_time[1]) * 1000000 + tonumber(redis_time[2])
local member = tostring(now_us) .. ':' .. ARGV[1]
local retry_us = 0

for index, key in ipairs(KEYS) do
  local argument_index = 2 + (index - 1) * 2
  local limit = tonumber(ARGV[argument_index])
  local window_ms = tonumber(ARGV[argument_index + 1])
  local window_us = window_ms * 1000

  redis.call('ZREMRANGEBYSCORE', key, '-inf', now_us - window_us)
  if redis.call('ZCARD', key) >= limit then
    local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
    if #oldest == 2 then
      local remaining = tonumber(oldest[2]) + window_us - now_us
      if remaining > retry_us then
        retry_us = remaining
      end
    end
  end
end

if retry_us > 0 then
  return {0, math.max(1, math.ceil(retry_us / 1000))}
end

for index, key in ipairs(KEYS) do
  local argument_index = 2 + (index - 1) * 2
  local window_ms = tonumber(ARGV[argument_index + 1])
  redis.call('ZADD', key, now_us, member)
  redis.call('PEXPIRE', key, window_ms + 1000)
end

return {1, 0}
`)
