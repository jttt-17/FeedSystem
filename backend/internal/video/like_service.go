// 更新点赞数、reids热榜、发送likemq和popularitymq消息
package video

import (
	"context"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jttt-17/feedsystem/backend/internal/middleware/rabbitmq"
	rediscache "github.com/jttt-17/feedsystem/backend/internal/middleware/redis"
	"gorm.io/gorm"
)

type LikeService struct {
	repo         *LikeRepository
	VideoRepo    *VideoRepository
	cache        *rediscache.Client
	likeMQ       *rabbitmq.LikeMQ
	popularityMQ *rabbitmq.PopularityMQ
}

func NewLikeService(repo *LikeRepository, videoRepo *VideoRepository, cache *rediscache.Client, likeMQ *rabbitmq.LikeMQ, popularityMQ *rabbitmq.PopularityMQ) *LikeService {
	return &LikeService{repo: repo, VideoRepo: videoRepo, cache: cache, likeMQ: likeMQ, popularityMQ: popularityMQ}
}

// 判断是否重复键错误（重复点赞）
func isDupKey(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

func (s *LikeService) Like(ctx context.Context, like *Like) error {
	if like == nil {
		return errors.New("like is nil")
	}
	if like.VideoID == 0 || like.AccountID == 0 {
		return errors.New("video_id and account_id are required")
	}

	//检查视频是否存在
	if s.VideoRepo != nil {
		ok, err := s.VideoRepo.IsExist(ctx, like.VideoID)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("video not found")
		}
	}

	//检查用户是否已经点赞
	isLiked, err := s.repo.IsLiked(ctx, like.VideoID, like.AccountID)
	if err != nil {
		return err
	}
	if isLiked {
		return errors.New("user has liked this video")
	}

	like.CreatedAt = time.Now()
	mysqlEnqueued := false
	redisEnqueued := false

	//若消息发送成功，则由消费者更新数据库中video表的like_count、video的Popularity，like表的点赞关系
	if s.likeMQ != nil {
		if err := s.likeMQ.Like(ctx, like.AccountID, like.VideoID); err == nil {
			mysqlEnqueued = true
		}
	}

	//若消息发送成功，则由消费者更新redis中的热度相关内容
	if s.popularityMQ != nil {
		if err := s.popularityMQ.Update(ctx, like.VideoID, 1); err == nil {
			redisEnqueued = true
		}
	}

	//likeMQ和popularityMQ都发送，则service不再操作数据库和redis
	if mysqlEnqueued && redisEnqueued {
		return nil
	}

	//likeMQ发送失败
	if !mysqlEnqueued {
		err := s.repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {

			//再次检查视频是否存在，上次检查和此时之间可能发生了变化如视频被删除，所以要再次检查
			if err := tx.Select("id").First(&Video{}, like.VideoID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errors.New("video not found")
				}
				return err
			}

			//插入点赞记录
			if err := tx.Create(like).Error; err != nil {
				if isDupKey(err) {
					return errors.New("user has liked this video")
				}
				return err
			}
			//更新video中的点赞数
			if err := tx.Model(&Video{}).Where("id = ?", like.VideoID).
				UpdateColumn("likes_count", gorm.Expr("likes_count + 1")).Error; err != nil {
				return err
			}
			//更新video中的热度
			return tx.Model(&Video{}).Where("id = ?", like.VideoID).
				UpdateColumn("popularity", gorm.Expr("popularity + 1")).Error
		})
		if err != nil {
			return err
		}
	}

	//popularityMQ发送失败
	if !redisEnqueued {
		UpdatePopularityCache(ctx, s.cache, like.VideoID, 1)
	}
	return nil

}

func (s *LikeService) Unlike(ctx context.Context, like *Like) error {
	if like == nil {
		return errors.New("like is nil")
	}
	if like.VideoID == 0 || like.AccountID == 0 {
		return errors.New("video_id and account_id are required")
	}

	//判断视频是否存在
	if s.VideoRepo != nil {
		ok, err := s.VideoRepo.IsExist(ctx, like.VideoID)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("video not found")
		}
	}

	//是否点赞了
	isLiked, err := s.repo.IsLiked(ctx, like.VideoID, like.AccountID)
	if err != nil {
		return err
	}
	if !isLiked {
		return errors.New("user has not liked this video")
	}

	mysqlEnqueued := false
	redisEnqueued := false
	if s.likeMQ != nil {
		if err := s.likeMQ.Unlike(ctx, like.AccountID, like.VideoID); err == nil {
			mysqlEnqueued = true
		}
	}
	if s.popularityMQ != nil {
		if err := s.popularityMQ.Update(ctx, like.VideoID, -1); err == nil {
			redisEnqueued = true
		}
	}
	if mysqlEnqueued && redisEnqueued {
		return nil
	}

	if !mysqlEnqueued {
		err := s.repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {

			//再次判断视频是否存在
			if err := tx.Select("id").First(&Video{}, like.VideoID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errors.New("video not found")
				}
				return err
			}
			//执行删除操作，Delete执行时，若发现要删除的键不存在，不会返回错误
			del := tx.Where("video_id = ? AND account_id = ?", like.VideoID, like.AccountID).Delete(&Like{})
			if del.Error != nil {
				return del.Error
			}
			//影响行数为0则说明未删除任何记录，则说明用户没有点赞这个视频
			if del.RowsAffected == 0 {
				return errors.New("user has not liked this video")
			}

			//更新like_count
			if err := tx.Model(&Video{}).Where("id = ?", like.VideoID).
				UpdateColumn("likes_count", gorm.Expr("GREATEST(likes_count - 1, 0)")).Error; err != nil {
				return err
			}
			//更新popularity
			return tx.Model(&Video{}).Where("id = ?", like.VideoID).
				UpdateColumn("popularity", gorm.Expr("GREATEST(popularity - 1, 0)")).Error
		})
		if err != nil {
			return err
		}
	}

	if !redisEnqueued {
		UpdatePopularityCache(ctx, s.cache, like.VideoID, -1)
	}
	return nil
}

func (s *LikeService) IsLiked(ctx context.Context, videoID, accountID uint) (bool, error) {
	return s.repo.IsLiked(ctx, videoID, accountID)
}

func (s *LikeService) ListLikedVideos(ctx context.Context, accountID uint) ([]Video, error) {
	return s.repo.ListLikedVideos(ctx, accountID)
}
