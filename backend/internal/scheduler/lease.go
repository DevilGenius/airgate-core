package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrLeaseLost = errors.New("执行租约已丢失")

func SlotTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return defaultSlotTTL
	}
	if ttl < time.Second {
		return time.Second
	}
	return ttl
}

// MaintainLease owns only this resource's child context. Canceling or stopping
// it releases the renewal worker; a failed renewal never reacquires a lost slot.
func MaintainLease(parent context.Context, ttl time.Duration, renew func(context.Context) (bool, error)) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	interval := min(ttl/3, defaultSlotTTL/3)
	if interval <= 0 {
		interval = time.Second
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				attempt, stop := context.WithTimeout(ctx, min(interval, redisAdmissionTimeout))
				owned, err := renew(attempt)
				stop()
				if ctx.Err() != nil {
					return
				}
				if err != nil || !owned {
					cancel(fmt.Errorf("%w: renewal failed", ErrLeaseLost))
					return
				}
			}
		}
	}()
	return ctx, func() { cancel(context.Canceled); <-done }
}

// The original ZSET retains heartbeat scores for compatibility. The companion
// ZSET stores a deadline for each token, independent of the next caller's TTL.
// Existing tokens are conservatively adopted on first access; no migration is
// needed and both keys expire with their longest retained lease.
const slotLeaseHelpers = `
local function leaseNow()
  local t=redis.call('TIME')
  return tonumber(t[1])+tonumber(t[2])/1000000
end
local function cleanSlotLeases(slotKey,now,fallbackTTL)
  local leases=slotKey..':leases'
  if redis.call('EXISTS',leases)==0 then
    local legacy=redis.call('ZRANGE',slotKey,0,-1,'WITHSCORES')
    for i=1,#legacy,2 do
      redis.call('ZADD',leases,tonumber(legacy[i+1])+math.max(300,fallbackTTL),legacy[i])
    end
  end
  local expired=redis.call('ZRANGEBYSCORE',leases,'-inf',now)
  local removed=0
  for _,token in ipairs(expired) do
    removed=removed+redis.call('ZREM',slotKey,token)
    redis.call('ZREM',leases,token)
  end
  -- Also clean legacy writers that inserted after the companion key was made.
  local legacy=redis.call('ZRANGEBYSCORE',slotKey,'-inf',now-math.max(300,fallbackTTL))
  for _,token in ipairs(legacy) do
    if not redis.call('ZSCORE',leases,token) then removed=removed+redis.call('ZREM',slotKey,token) end
  end
  return removed
end
local function refreshSlotLeaseKeys(slotKey,countKey,now,current)
  local leases=slotKey..':leases'
  local first=redis.call('ZRANGE',leases,0,0,'WITHSCORES')
  local last=redis.call('ZREVRANGE',leases,0,0,'WITHSCORES')
  local keepTTL=300
  local countTTL=300
  if #last>0 then keepTTL=math.max(keepTTL,math.ceil(tonumber(last[2])-now)) end
  if #first>0 then countTTL=math.max(1,math.min(300,math.ceil(tonumber(first[2])-now))) end
  redis.call('EXPIRE',slotKey,keepTTL)
  redis.call('EXPIRE',leases,keepTTL)
  redis.call('SET',countKey,current,'EX',countTTL)
end
`

var renewSlotScript = redis.NewScript(slotLeaseHelpers + `
local now=leaseNow()
local key=KEYS[1]
local token=ARGV[1]
local ttl=tonumber(ARGV[2])
local expiry=redis.call('ZSCORE',key..':leases',token)
if not expiry or tonumber(expiry)<=now or not redis.call('ZSCORE',key,token) then return 0 end
cleanSlotLeases(key,now,ttl)
redis.call('ZADD',key,now,token)
redis.call('ZADD',key..':leases',now+ttl,token)
refreshSlotLeaseKeys(key,KEYS[2],now,redis.call('ZCARD',key))
return 1
`)

func (cm *ConcurrencyManager) Distributed() bool { return cm != nil && cm.rdb != nil }
func (cm *ConcurrencyManager) renewSlotByKey(ctx context.Context, key, countKey, requestID string, ttl time.Duration) (bool, error) {
	if !cm.Distributed() {
		return true, nil
	}
	ctx, cancel := context.WithTimeout(ctx, redisAdmissionTimeout)
	defer cancel()
	result, err := renewSlotScript.Run(ctx, cm.rdb, []string{key, countKey}, requestID, int(SlotTTL(ttl).Seconds())).Int()
	return result == 1, err
}
func (cm *ConcurrencyManager) RenewSlot(ctx context.Context, accountID int, requestID string, ttl time.Duration) (bool, error) {
	return cm.renewSlotByKey(ctx, concurrencyKey(accountID), concurrencyCountKey(accountID), requestID, ttl)
}
func (cm *ConcurrencyManager) RenewUserSlot(ctx context.Context, userID int, requestID string, ttl time.Duration) (bool, error) {
	return cm.renewSlotByKey(ctx, userConcurrencyKey(userID), userConcurrencyCountKey(userID), requestID, ttl)
}
func (cm *ConcurrencyManager) RenewAPIKeySlot(ctx context.Context, keyID int, requestID string, ttl time.Duration) (bool, error) {
	return cm.renewSlotByKey(ctx, apiKeyConcurrencyKey(keyID), apiKeyConcurrencyCountKey(keyID), requestID, ttl)
}

var renewMessageLockScript = redis.NewScript(`
if redis.call('GET',KEYS[1])==ARGV[1] then
  redis.call('PEXPIRE',KEYS[1],ARGV[2])
  return 1
end
return 0
`)

func MessageLockTTL(extra map[string]interface{}) time.Duration {
	if seconds := ExtraInt(extra, "msg_lock_ttl_seconds"); seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return defaultLockTTL
}
func (s *Scheduler) RenewMessageLock(ctx context.Context, accountID int, requestID string, ttl time.Duration) (bool, error) {
	if s.msgQueue.rdb == nil {
		return true, nil
	}
	result, err := renewMessageLockScript.Run(ctx, s.msgQueue.rdb, []string{msgQueueLockKey(accountID)}, requestID, ttl.Milliseconds()).Int()
	return result == 1, err
}
