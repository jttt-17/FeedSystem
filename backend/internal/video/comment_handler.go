package video

import (
	"github.com/gin-gonic/gin"
	"github.com/jttt-17/feedsystem/backend/internal/account"
	"github.com/jttt-17/feedsystem/backend/internal/apierror"
	"github.com/jttt-17/feedsystem/backend/internal/middleware/jwt"
)

type CommentHandler struct {
	service        *CommentService
	accountService *account.AccountService
}

func NewCommentHandler(service *CommentService, accountService *account.AccountService) *CommentHandler {
	return &CommentHandler{service: service, accountService: accountService}
}

func (h *CommentHandler) PublishComment(c *gin.Context) {
	var req PublishCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{"error": err.Error()})
		return
	}

	if req.Content == "" {
		c.JSON(400, gin.H{"error": "content is required"})
		return
	}
	if req.VideoID <= 0 {
		c.JSON(400, gin.H{"error": "video_id is required"})
		return
	}

	//从请求中获取用户id，发表评论人的id
	authorId, err := jwt.GetAccountID(c)
	if err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{"error": err.Error()})
		return
	}

	//由userid获取user信息
	user, err := h.accountService.FindByID(c.Request.Context(), authorId)
	if err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{"error": err.Error()})
		return
	}

	comment := &Comment{
		Username: user.Username,
		VideoID:  req.VideoID,
		AuthorID: authorId, //评论人的id
		Content:  req.Content,
	}

	if err := h.service.Publish(c.Request.Context(), comment); err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{"error": err.Error()})
		return
	}

}

func (h *CommentHandler) DeleteComment(c *gin.Context) {
	var req DeleteCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{"error": err.Error()})
		return
	}
	accountID, err := jwt.GetAccountID(c)
	if err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{"error": err.Error()})
		return
	}
	if req.CommentID <= 0 {
		c.JSON(400, gin.H{"error": "comment_id is required"})
		return
	}
	if err := h.service.Delete(c.Request.Context(), req.CommentID, accountID); err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "comment deleted successfully"})
}

// 获取最早的200条评论
func (h *CommentHandler) GetAllComments(c *gin.Context) {
	var req GetAllCommentsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{"error": err.Error()})
		return
	}
	if req.VideoID == 0 {
		c.JSON(400, gin.H{"error": "video_id is required"})
		return
	}
	comments, err := h.service.GetAll(c.Request.Context(), req.VideoID)
	if err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{"error": err.Error()})
		return
	}
	if comments == nil {
		comments = []Comment{}
	}
	c.JSON(200, comments)
}
