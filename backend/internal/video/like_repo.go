package video

import (
	"context"
	"errors"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

type LikeRepository struct {
	db *gorm.DB
}

func NewLikeRepository(db *gorm.DB) *LikeRepository {
	return &LikeRepository{db: db}
}

// 没有更新video表中的like_count
func (r *LikeRepository) Like(ctx context.Context, like *Like) error {
	return r.db.WithContext(ctx).Create(like).Error
}

func (r *LikeRepository) Unlike(ctx context.Context, like *Like) error {
	return r.db.WithContext(ctx).
		Where("video_id = ? AND account_id = ?", like.VideoID, like.AccountID).
		Delete(&Like{}).Error
}

// 点赞是否成功，成功则true，重复点赞则false
func (r *LikeRepository) LikeIgnoreDuplicate(ctx context.Context, like *Like) (created bool, err error) {
	if like == nil || like.VideoID == 0 || like.AccountID == 0 {
		return false, nil
	}
	err = r.db.WithContext(ctx).Create(like).Error
	if err == nil {
		return true, nil
	}
	var mysqlErr *mysql.MySQLError
	//重复插入则MySQL返回1062错误
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return false, nil
	}
	return false, err
}

func (r *LikeRepository) DeleteByVideoAndAccount(ctx context.Context, videoID, accountID uint) (deleted bool, err error) {
	if videoID == 0 || accountID == 0 {
		return false, nil
	}
	res := r.db.WithContext(ctx).
		Where("video_id = ? AND account_id = ?", videoID, accountID).
		Delete(&Like{})
	return res.RowsAffected > 0, res.Error
}

func (r *LikeRepository) IsLiked(ctx context.Context, videoID uint, accountID uint) (is_liked bool, err error) {
	var count int64
	err = r.db.WithContext(ctx).Model(&Like{}).Where("video_id=? AND account_id=?", videoID, accountID).Count(&count).Error

	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// 查看某个人对某些视频是否点了赞
func (r *LikeRepository) BatchGetLiked(ctx context.Context, videoIDs []uint, accountID uint) (map[uint]bool, error) {
	likeMap := make(map[uint]bool)
	if len(videoIDs) == 0 {
		return likeMap, nil
	}
	if accountID == 0 {
		return likeMap, nil
	}
	var likes []Like
	err := r.db.WithContext(ctx).Model(&Like{}).Where("account_id=? AND video_id IN ?", accountID, videoIDs).Find(&likes).Error
	if err != nil {
		return nil, err
	}

	for _, like := range likes {
		likeMap[like.VideoID] = true
	}
	return likeMap, nil
}

// 查找某个人所有点赞的视频
func (r *LikeRepository) ListLikedVideos(ctx context.Context, accountID uint) ([]Video, error) {
	var videos []Video
	if accountID == 0 {
		return videos, nil
	}
	err := r.db.WithContext(ctx).
		Model(&Video{}).
		Joins("JOIN likes ON likes.video_id = videos.id"). //连接likes表
		Where("likes.account_id = ?", accountID).
		Order("likes.created_at desc").
		Limit(200).
		Find(&videos).Error
	if err != nil {
		return nil, err
	}
	return videos, nil
}
