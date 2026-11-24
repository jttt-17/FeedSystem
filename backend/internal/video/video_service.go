// 数据检验、读写redis、rabitmq
package video

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jttt-17/feedsystem/backend/internal/apierror"
	"github.com/jttt-17/feedsystem/backend/internal/middleware/rabbitmq"
	rediscache "github.com/jttt-17/feedsystem/backend/internal/middleware/redis"
	"gorm.io/gorm"
)

type VideoService struct {
	repo         *VideoRepository
	cache        *rediscache.Client
	cacheTTL     time.Duration
	popularityMQ *rabbitmq.PopularityMQ
}

func NewVideoService(repo *VideoRepository, cache *rediscache.Client, popularityMQ *rabbitmq.PopularityMQ) *VideoService {
	return &VideoService{repo: repo, cache: cache, cacheTTL: 5 * time.Minute, popularityMQ: popularityMQ}
}

func (vs *VideoService) Publish(ctx context.Context, video *Video) error {
	if video == nil {
		return errors.New("video is nil")
	}

	//去除首尾空白
	video.Title = strings.TrimSpace(video.Title)
	video.PlayURL = strings.TrimSpace(video.PlayURL)
	video.CoverURL = strings.TrimSpace(video.CoverURL)

	if video.Title == "" {
		return errors.New("title is required")
	}
	if video.PlayURL == "" {
		return errors.New("play url is required")
	}
	if video.CoverURL == "" {
		return errors.New("cover url is required")
	}

	err := vs.repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(video).Error; err != nil {
			return err
		}

		//回写CreateTime: video.CreateTime
		msg := OutboxMsg{
			VideoID:    video.ID,
			EventType:  "video_published",
			Status:     "pending",
			CreateTime: video.CreateTime,
		}

		if err := tx.Create(&msg).Error; err != nil {
			return err
		}

		tags := ExtractTags(video.Title + " " + video.Description)
		for _, tagName := range tags {
			var tag Tag

			//操作Tag表，FirstOrCreate：查到了则不写入，返回结果到变量tag中，没查到则把这个标签写入表中，再返回结果给变量tag
			if err := tx.Where("name=?", tagName).FirstOrCreate(&tag, Tag{Name: tagName}).Error; err != nil {
				return err
			}

			//写videotag表
			if err := tx.Create(&VideoTag{VideoID: video.ID, TagID: tag.ID}).Error; err != nil {
				return err
			}
		}
		return nil

	})
	return err
}

// 只删掉了video中的数据，tag和videotag中的相关数据没有删掉
func (vs *VideoService) Delete(ctx context.Context, id uint, authorID uint) error {
	video, err := vs.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if video.AuthorID != authorID {
		return apierror.ErrUnauthorized
	}
	if err := vs.repo.DeleteVideo(ctx, id); err != nil {
		return err
	}

	if vs.cache != nil {
		cacheKey := vs.cache.Key("video:detail:id=%d", id)
		//忽略删除redis缓存返回的错误，即使缓存失效，Delete方法仍然返回成功
		//可以避免缓存故障影响数据库删除，但可能导致旧缓存继续存在，直到它自然过期。
		//context.Background()不继承当前请求取消信号的上下文：即使 HTTP 请求已经取消，代码仍然尝试清理缓存
		//若采用全局上下文，当一个请求删掉某个视频，sql中删掉后，请求终止，redis没有删掉，会有缓存数据库不一致的情况
		_ = vs.cache.Del(context.Background(), cacheKey)
	}

	return nil
}

func (vs *VideoService) ListByAuthorID(ctx context.Context, authorID uint) ([]Video, error) {
	videos, err := vs.repo.ListByAuthorID(ctx, authorID)
	if err != nil {
		return nil, err
	}
	return videos, nil
}

func (vs *VideoService) GetDetail(ctx context.Context, id uint) (*Video, error) {
	cacheKey := vs.cache.Key("video:detail:id=%d", id)

	//匿名函数
	getCached := func() (*Video, error) {
		opCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()

		b, err := vs.cache.GetBytes(opCtx, cacheKey)
		if err != nil {
			return nil, err
		}
		var cached Video
		if err := json.Unmarshal(b, &cached); err != nil {
			return nil, err
		}
		return &cached, nil
	}

	setCached := func(video *Video) {
		b, err := json.Marshal(video)
		if err != nil {
			return
		}
		opCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		_ = vs.cache.SetBytes(opCtx, cacheKey, b, vs.cacheTTL)
	}

	if vs.cache != nil {
		v, err := getCached()

		if err == nil {
			//redis查到了
			return v, nil
		} else if rediscache.IsMiss(err) {
			//缓存击穿保护，不是为了保护视频数据本身
			lockKey := "lock:" + cacheKey

			lockCtx, lockCancel := context.WithTimeout(ctx, 50*time.Millisecond)
			token, locked, lockErr := vs.cache.Lock(lockCtx, lockKey, 2*time.Second)
			lockCancel()

			if lockErr == nil && locked {
				defer func() { _ = vs.cache.Unlock(context.Background(), lockKey, token) }()

				//再次查询作用：若a,b同时访问，发现redis没有，然后同时去拿锁，a拿到锁，再次查询发现redis还是没有，则a查询数据库后，更新redis，a释放锁，b拿到锁后，再次查询发现redis中已经有了，就不用再查询数据库。
				if v, err := getCached(); err == nil {
					return v, nil
				}

				//查询数据库兜底
				video, err := vs.repo.GetByID(ctx, id)
				if err != nil {
					return nil, err
				}

				//更新redis
				setCached(video)
				return video, nil
			}

			// 没拿到锁：等待别人回填缓存
			for i := 0; i < 5; i++ {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(20 * time.Millisecond):
				}
				if v, err := getCached(); err == nil {
					return v, nil
				}
			}

		}

	}

	//等待别人回填后，发现redis还是查不到，则去查数据库
	video, err := vs.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if vs.cache != nil {
		setCached(video)
	}
	return video, nil

}

func (vs *VideoService) UpdateLikesCount(ctx context.Context, id uint, likesCount int64) error {
	if err := vs.repo.UpdateLikesCount(ctx, id, likesCount); err != nil {
		return err
	}
	return nil
}

func (vs *VideoService) UpdatePopularity(ctx context.Context, id uint, change int64) error {
	if err := vs.repo.UpdatePopularity(ctx, id, change); err != nil {
		return err
	}

	if vs.popularityMQ != nil {
		//发送消息成功则不再修改redis，后续消费者应该修改redis
		if err := vs.popularityMQ.Update(ctx, id, change); err == nil {
			return nil
		}
	}

	if vs.cache != nil {
		//删除缓存
		_ = vs.cache.Del(context.Background(), vs.cache.Key("video:detail:id=%d", id))

		//
		now := time.Now().UTC().Truncate(time.Minute) //Truncate(time.Minute):把时间截断到分钟
		windowKey := vs.cache.Key("hot:video:1m:%s", now.Format("200601021504"))
		member := strconv.FormatUint(uint64(id), 10)

		opCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()

		//对有序集合windowKey中的member成员的score加上change的变化量
		_ = vs.cache.ZincrBy(opCtx, windowKey, member, float64(change))

		//热度窗口的过期时间设置为2小时，每次有热度更新就会刷新这个时间，也就是重新开始2小时
		_ = vs.cache.Expire(opCtx, windowKey, 2*time.Hour)
	}
	return nil
}
