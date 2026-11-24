package video

import (
	"context"
	"errors"
	"log"
	"regexp"
	"strings"

	"github.com/jttt-17/feedsystem/backend/internal/middleware/rabbitmq"

	"github.com/jttt-17/feedsystem/backend/internal/apierror"
	rediscache "github.com/jttt-17/feedsystem/backend/internal/middleware/redis"
	"gorm.io/gorm"
)

type CommentService struct {
	repo            *CommentRepository
	VideoRepository *VideoRepository
	cache           *rediscache.Client
	commentMQ       *rabbitmq.CommentMQ
	popularityMQ    *rabbitmq.PopularityMQ
}

func NewCommentService(repo *CommentRepository, videoRepo *VideoRepository, cache *rediscache.Client, commentMQ *rabbitmq.CommentMQ, popularityMQ *rabbitmq.PopularityMQ) *CommentService {
	return &CommentService{repo: repo, VideoRepository: videoRepo, cache: cache, commentMQ: commentMQ, popularityMQ: popularityMQ}
}

func (s *CommentService) Publish(ctx context.Context, comment *Comment) error {
	if comment == nil {
		return errors.New("comment is nil")
	}
	comment.Username = strings.TrimSpace(comment.Username)
	comment.Content = strings.TrimSpace(comment.Content)
	if comment.VideoID == 0 || comment.AuthorID == 0 {
		return errors.New("video_id and author_id are required")
	}
	if comment.Content == "" {
		return errors.New("content is required")
	}

	//判断视频是否存在
	exists, err := s.VideoRepository.IsExist(ctx, comment.VideoID)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("video not found")
	}

	mysqlEnqueued := false
	redisEnqueued := false
	if s.commentMQ != nil {
		if err := s.commentMQ.Publish(ctx, comment.Username, comment.VideoID, comment.AuthorID, comment.Content); err == nil {
			mysqlEnqueued = true
		}
	}
	if s.popularityMQ != nil {
		if err := s.popularityMQ.Update(ctx, comment.VideoID, 1); err == nil {
			redisEnqueued = true
		}
	}
	//@提及通知可能早于评论落库
	//更稳妥的设计是：由评论消费者在评论成功写入数据库后，再创建对应的 @提及通知
	if mysqlEnqueued && redisEnqueued {
		s.notifyMentions(ctx, comment)
		return nil
	}

	//commentMQ.Publish失败
	if !mysqlEnqueued {
		if err := s.repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			//再次检查视频是否存在
			if err := tx.Select("id").First(&Video{}, comment.VideoID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errors.New("video not found")
				}
				return err
			}
			//comment表更新
			if err := tx.Create(comment).Error; err != nil {
				return err
			}
			//video表热度更新
			return tx.Model(&Video{}).Where("id = ?", comment.VideoID).
				UpdateColumn("popularity", gorm.Expr("popularity + 1")).Error
		}); err != nil {
			return err
		}
	}

	//popularityMQ.Update失败
	if !redisEnqueued {
		UpdatePopularityCache(ctx, s.cache, comment.VideoID, 1)
	}
	s.notifyMentions(ctx, comment)
	return nil

}

// 删除评论不影响popularity
func (s *CommentService) Delete(ctx context.Context, commentID uint, accountID uint) error {
	comment, err := s.repo.GetByID(ctx, commentID)
	if err != nil {
		return err
	}
	if comment == nil {
		return errors.New("comment not found")
	}
	if comment.AuthorID != accountID {
		return apierror.ErrUnauthorized
	}
	if s.commentMQ != nil {
		if err := s.commentMQ.Delete(ctx, commentID); err == nil {
			return nil
		}
	}
	//commentMQ.Delete失败则repo兜底
	return s.repo.DeleteComment(ctx, comment)
}

// 获取最早的200条评论
func (s *CommentService) GetAll(ctx context.Context, videoID uint) ([]Comment, error) {
	exists, err := s.VideoRepository.IsExist(ctx, videoID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New("video not found")
	}
	return s.repo.GetAllComments(ctx, videoID)
}

var mentionRegex = regexp.MustCompile(`@(\w+)`)

func (s *CommentService) notifyMentions(ctx context.Context, comment *Comment) {
	matches := mentionRegex.FindAllStringSubmatch(comment.Content, -1) //-1表示不限制匹配到的数量
	//matches的格式：
	// 	matches = [][]string{
	//     {"@Alice", "Alice"},
	//     {"@Bob", "Bob"},
	//     {"@Tom", "Tom"},
	// }
	if len(matches) == 0 {
		return
	}
	//使用map去重
	seen := make(map[string]bool)
	for _, m := range matches {
		username := m[1]
		if seen[username] || username == comment.Username {
			continue
		}
		seen[username] = true
		var accID uint
		if err := s.repo.db.WithContext(ctx).Table("accounts").Where("username = ?", username).Select("id").Scan(&accID).Error; err != nil || accID == 0 {
			continue
		}
		notif := struct {
			RecipientID uint
			SenderID    uint
			Type        string
			TargetID    uint
			Content     string
		}{
			RecipientID: accID,            //被@的人
			SenderID:    comment.AuthorID, //发评论的人
			Type:        "mention",
			TargetID:    comment.VideoID,
			Content:     comment.Username + " 在评论中提到了你",
		}
		if err := s.repo.db.WithContext(ctx).Table("notifications").Create(&notif).Error; err != nil {
			//没有return err，即：评论发送成功，@功能失败不会影响到评论发布
			log.Printf("create mention notification failed: %v", err)
		}
	}
}
