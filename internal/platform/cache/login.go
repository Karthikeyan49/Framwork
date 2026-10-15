package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/redis/go-redis/v9"
)

var loginKey = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Each SET carries its own TTL, even if a later command fails under ACL drift.
// Counters and insertion are atomic against parallel requests. No raw IP, cookie,
// token or business data is stored in a key.
const saveLogin = `
local g=tonumber(redis.call('GET',KEYS[2]) or '0')
local p=tonumber(redis.call('GET',KEYS[3]) or '0')
if not g or not p then return -1 end
if g>=120 or p>=20 then return 0 end
local gt=redis.call('TTL',KEYS[2]); if gt<1 or gt>60 then gt=60 end
local pt=redis.call('TTL',KEYS[3]); if pt<1 or pt>60 then pt=60 end
redis.call('SET',KEYS[2],g+1,'EX',gt)
redis.call('SET',KEYS[3],p+1,'EX',pt)
if redis.call('SET',KEYS[1],ARGV[1],'EX',300,'NX') then return 1 end
return -1`

func (c *Client) SaveLoginFlow(ctx context.Context, key, remote string, flow identity.LoginFlow) error {
	if !loginKey.MatchString(key) || !loginKey.MatchString(remote) {
		return identity.ErrLoginUnavailable
	}
	raw, err := json.Marshal(flow)
	if err != nil || len(raw) > 2048 {
		return identity.ErrLoginUnavailable
	}
	result, err := c.client.Eval(ctx, saveLogin, []string{"framework:signin:v1:flow:" + key, "framework:signin:v1:global", "framework:signin:v1:remote:" + remote}, string(raw)).Int()
	if err != nil {
		return identity.ErrLoginUnavailable
	}
	if result == 0 {
		return identity.ErrLoginBusy
	}
	if result != 1 {
		return identity.ErrLoginUnavailable
	}
	return nil
}
func (c *Client) TakeLoginFlow(ctx context.Context, key string) (identity.LoginFlow, error) {
	var flow identity.LoginFlow
	if !loginKey.MatchString(key) {
		return flow, identity.ErrUnauthenticated
	}
	raw, err := c.client.GetDel(ctx, "framework:signin:v1:flow:"+key).Bytes()
	if errors.Is(err, redis.Nil) {
		return flow, identity.ErrUnauthenticated
	}
	if err != nil {
		return flow, identity.ErrLoginUnavailable
	}
	if len(raw) > 2048 || json.Unmarshal(raw, &flow) != nil {
		return flow, identity.ErrUnauthenticated
	}
	return flow, nil
}

// Probe only a short-lived random reserved key, never a flow or admission counter.
func (c *Client) ReadyLoginFlows(ctx context.Context) error {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return identity.ErrLoginUnavailable
	}
	suffix := hex.EncodeToString(raw)
	if err := c.client.Get(ctx, "framework:cache:signin-negative:"+suffix).Err(); !redis.HasErrorPrefix(err, "NOPERM") {
		return identity.ErrLoginUnavailable
	}
	if err := c.client.Do(ctx, "ACL", "CAT").Err(); !redis.HasErrorPrefix(err, "NOPERM") {
		return identity.ErrLoginUnavailable
	}
	value, err := c.client.Eval(ctx, `redis.call('SET',KEYS[1],'probe','EX',5); local ttl=redis.call('TTL',KEYS[1]); local v=redis.call('GETDEL',KEYS[1]); if ttl>0 and v=='probe' then return 1 end; return 0`, []string{"framework:signin:v1:probe:" + suffix}).Int()
	if err != nil || value != 1 {
		return identity.ErrLoginUnavailable
	}
	return nil
}
