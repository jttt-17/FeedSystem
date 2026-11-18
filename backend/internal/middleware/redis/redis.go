// redis的声明、上锁解锁等等
package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jttt-17/feedsystem/backend/internal/config"
	redis "github.com/redis/go-redis/v9"
)

type Client struct {
	rdb       *redis.Client
	keyPrefix string //前缀：避免key命名混乱
}

const defaultKeyPrefix = "v1:"

func NewClient(rdb *redis.Client, keyPrefix string) *Client {
	return &Client{
		rdb:       rdb,
		keyPrefix: keyPrefix,
	}
}

func NewFromEnv(cfg *config.RedisConfig) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Host + ":" + strconv.Itoa(cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	return &Client{rdb: rdb, keyPrefix: defaultKeyPrefix}, nil
}

func (c *Client) Close() error {
	if c == nil || c.rdb == nil {
		return nil
	}
	return c.rdb.Close()
}

func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.rdb == nil {
		return errors.New("redis client not initialized")
	}
	return c.rdb.Ping(ctx).Err()
}

// 等价于errors.Is(err,redis.Nil)
func IsMiss(err error) bool {
	return err == redis.Nil
}

// 加前缀
func (c *Client) Key(format string, args ...any) string {
	prefix := ""
	if c != nil {
		prefix = c.keyPrefix
	}
	return prefix + fmt.Sprintf(format, args...)

}

func randToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// 参数key为上锁的资源
func (c *Client) Lock(ctx context.Context, key string, ttl time.Duration) (token string, ok bool, err error) {
	if c == nil || c.rdb == nil {
		return "", false, nil
	}
	//每次的锁都是全局唯一的
	//可以防止一种情况：若锁名固定为lock，A、B同时抢锁，A成功抢到，ttl为10s
	//若A故障执行时间为12s，当10s时刻，lock失效，B拿到锁，12s时刻，A干完活无脑释放lock，那么其他进程就是拿到锁和B争夺资源。
	//这里没有考虑续期，即某个任务执行时间超过ttl，锁过期导致被其他进程拿到，造成数据竞争
	token, err = randToken(16)
	if err != nil {
		return "", false, err
	}

	//SetNX是原子操作
	//把(key:token)存在redis中
	//正常情况下由上锁的进程释放锁
	//异常情况下由redis删掉锁
	ok, err = c.rdb.SetNX(ctx, key, token, ttl).Result()
	return token, ok, err
}

// Lua脚本，原子性
var unlockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
else
  return 0
end
`)

func (c *Client) Unlock(ctx context.Context, key string, token string) error {
	if c == nil || c.rdb == nil {
		return nil
	}
	_, err := unlockScript.Run(ctx, c.rdb, []string{key}, token).Result()
	return err
}

var incrementWithExpireScript = redis.NewScript(`
local count = redis.call("INCR", KEYS[1])
if count == 1 then
  redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
return count
`)

// 限流
func (c *Client) IncrementWithExpire(ctx context.Context, key string, expire time.Duration) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, nil
	}
	return incrementWithExpireScript.Run(
		ctx,
		c.rdb,
		[]string{key},
		expire.Milliseconds(),
	).Int64()
}
