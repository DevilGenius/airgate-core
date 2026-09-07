package scheduler

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

var clientRPMScript = redis.NewScript(`
local second=redis.call('TIME')[1]
local minute=math.floor(tonumber(second)/60)
local current=tonumber(redis.call('HGET',KEYS[1],'minute') or '-1')
local total=0
local other=0
if current==minute then
  total=tonumber(redis.call('HGET',KEYS[1],'total') or '0')
  other=tonumber(redis.call('HGET',KEYS[1],'other') or '0')
end
local totalMax=tonumber(ARGV[1])
local otherMax=tonumber(ARGV[2])
local countOther=tonumber(ARGV[3])==1
if totalMax>0 and total>=totalMax then return 0 end
if countOther and otherMax>0 and other>=otherMax then return 0 end
if totalMax>0 then total=total+1 end
if countOther and otherMax>0 then other=other+1 end
redis.call('HSET',KEYS[1],'minute',minute,'total',total,'other',other)
redis.call('EXPIRE',KEYS[1],120)
return 1
`)

func clientRPMKey(keyID int) string { return fmt.Sprintf("ag:client:rpm:%d", keyID) }

func (s *Scheduler) AllowAPIKeyRPM(ctx context.Context, keyID, totalMax, otherMax int, countOther bool) (bool, error) {
	if s == nil || s.rdb == nil {
		return false, ErrSchedulingUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, redisAdmissionTimeout)
	defer cancel()
	flag := 0
	if countOther {
		flag = 1
	}
	result, err := clientRPMScript.Run(ctx, s.rdb, []string{clientRPMKey(keyID)}, totalMax, otherMax, flag).Int()
	if err != nil {
		return false, fmt.Errorf("%w: API Key RPM: %w", ErrSchedulingUnavailable, err)
	}
	return result == 1, nil
}
