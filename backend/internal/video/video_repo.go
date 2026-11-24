package video

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

type VideoRepository struct {
	db *gorm.DB
}

func NewVideoRepository(db *gorm.DB) *VideoRepository {
	return &VideoRepository{db: db}
}

func (vr *VideoRepository) CreateVideo(ctx context.Context, video *Video) error {
	if err := vr.db.WithContext(ctx).Create(video).Error; err != nil {
		return err
	}
	return nil
}

// 看不懂这个有什么用
func (vr *VideoRepository) CreateMsg(ctx context.Context, Msg *OutboxMsg) error {
	if err := vr.db.WithContext(ctx).Create(Msg).Error; err != nil {
		return err
	}
	return nil
}

func (vr *VideoRepository) DeleteVideo(ctx context.Context, videoID uint) error {
	if err := vr.db.WithContext(ctx).Delete(&Video{}, videoID).Error; err != nil {
		return err
	}
	return nil
}

func (vr *VideoRepository) ListByAuthorID(ctx context.Context, authorID uint) ([]Video, error) {
	var videos []Video
	if err := vr.db.WithContext(ctx).Where("author_id=?", authorID).Order("create_time desc").Limit(200).Find(&videos).Error; err != nil {
		return nil, err
	}
	return videos, nil

}

func (vr *VideoRepository) GetByID(ctx context.Context, videoID uint) (*Video, error) {
	var video Video
	if err := vr.db.WithContext(ctx).First(&video, videoID).Error; err != nil {
		return nil, err
	}
	return &video, nil
}

// 直接设为指定值
func (vr *VideoRepository) UpdateLikesCount(ctx context.Context, id uint, likesCount int64) error {
	if err := vr.db.WithContext(ctx).Model(&Video{}).
		Where("id = ?", id).
		Update("likes_count", likesCount).Error; err != nil {
		return err
	}
	return nil
}

func (vr *VideoRepository) IsExist(ctx context.Context, videoID uint) (bool, error) {
	_, err := vr.GetByID(ctx, videoID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil

}

// 原值上加change
func (vr *VideoRepository) UpdatePopularity(ctx context.Context, videoID uint, change int64) error {
	if err := vr.db.WithContext(ctx).Model(&Video{}).Where("id=?", videoID).Update("popularity", gorm.Expr("popularity+?", change)).Error; err != nil {
		return err
	}
	return nil
}

// 原值上加change，限制最小值为0
func (vr *VideoRepository) ChangeLikeCount(ctx context.Context, videoID uint, change int64) error {
	if err := vr.db.WithContext(ctx).Model(&Video{}).Where("id=?", videoID).UpdateColumn("likes_count", gorm.Expr("GREATEST(likes_count+?,0)", change)).Error; err != nil {
		return err
	}
	return nil
}

// 原值上加change，限制最小值为0
func (vr *VideoRepository) ChangePopularity(ctx context.Context, id uint, change int64) error {
	if err := vr.db.WithContext(ctx).Model(&Video{}).
		Where("id = ?", id).
		UpdateColumn("popularity", gorm.Expr("GREATEST(popularity + ?, 0)", change)).Error; err != nil {
		return err
	}
	return nil
}

func (vr *VideoRepository) CountByAuthor(ctx context.Context, authorID uint) (int64, error) {
	var count int64
	if err := vr.db.WithContext(ctx).Model(&Video{}).Where("author_id=?", authorID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (vr *VideoRepository) TotalLikesByAuthor(ctx context.Context, authorID uint) (int64, error) {
	var total int64
	//COALESCE:当sum(likes_count)结果为null，就返回0
	if err := vr.db.WithContext(ctx).Model(&Video{}).Where("author_id=?", authorID).Select("COALESCE(SUM(likes_count),0)").Scan(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}
