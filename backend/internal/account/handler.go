// 解析请求的参数
package account

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jttt-17/feedsystem/backend/internal/apierror"
	"gorm.io/gorm"
)

// 为什么构造结构体都是选择指针
type AccountHandler struct {
	accountService *AccountService
}

func NewAccountHandler(accountService *AccountService) *AccountHandler {
	return &AccountHandler{accountService: accountService}
}

func (h *AccountHandler) CreateAccount(c *gin.Context) {
	var req CreateAccountRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{
			"error": err.Error(),
		})
		return
	}
	if err := h.accountService.CreateAccount(c.Request.Context(), &Account{
		Username: req.Username,
		Password: req.Password,
	}); err != nil {
		c.JSON(500, gin.H{
			"error": err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"message": "account created",
	})

}

func (h *AccountHandler) Rename(c *gin.Context) {
	var req RenameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{"error": err.Error()})
		return
	}
	accountID, err := getAccountID(c)
	if err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{"error": err.Error()})
		return
	}
	token, err := h.accountService.Rename(c.Request.Context(), accountID, req.NewUsername)
	if err != nil {
		if errors.Is(err, ErrNewUsernameRequired) {
			c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, ErrUsernameTaken) {
			c.JSON(409, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(404, gin.H{"error": "account not found"})
			return
		}
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"token": token})
}

func (h *AccountHandler) ChangePassword(c *gin.Context) {
	var req ChangePasswordRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{
			"error": err.Error(),
		})
	}

	if err := h.accountService.ChangePassword(
		c.Request.Context(),
		req.Username,
		req.OldPassword,
		req.NewPassword,
	); err != nil {
		c.JSON(400, gin.H{
			"error": "unsuccessfully password changed",
		})
		return
	}
	c.JSON(200, gin.H{"message": "successfully password changed"})

}

func (h *AccountHandler) FindByID(c *gin.Context) {
	var req FindByIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{
			"error": err.Error(),
		})
	}

	if account, err := h.accountService.FindByID(c.Request.Context(), req.ID); err != nil {
		c.JSON(500, gin.H{
			"error": err.Error(),
		})
	} else {
		//敏感信息泄露
		c.JSON(200, account)
	}
}

func (h *AccountHandler) FindByUsername(c *gin.Context) {
	var req FindByUsernameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{
			"error": err.Error(),
		})
	}

	if account, err := h.accountService.FindByUsername(c.Request.Context(), req.Username); err != nil {
		c.JSON(500, gin.H{
			"error": err.Error(),
		})
	} else {
		//敏感信息泄露
		c.JSON(200, account)
	}
}

func (h *AccountHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{
			"error": err.Error(),
		})
	}

	//优化account获取，下面h.accountService.Login也会使用FindByUsername，可以更改一下login的返回值，增加account返回
	account, err := h.accountService.FindByUsername(c.Request.Context(), req.Username)
	if err != nil {
		c.JSON(
			500, gin.H{
				"error": err.Error(),
			},
		)
		return
	}

	accessToken, refreshToken, err := h.accountService.Login(
		c.Request.Context(),
		req.Username,
		req.Password,
	)
	if err != nil {
		c.JSON(500, gin.H{
			"error": err.Error(),
		})
		return
	}
	c.JSON(200, LoginResponse{
		Token:        accessToken,
		RefreshToken: refreshToken,
		AccountID:    account.ID,
		Username:     account.Username,
	})

}

func (h *AccountHandler) Logout(c *gin.Context) {
	accountID, err := getAccountID(c)
	if err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{
			"error": err.Error(),
		})
	}
	//删除redis中的缓存，清空mysql中的token和refreshtoken
	if err := h.accountService.Logout(c.Request.Context(), accountID); err != nil {
		c.JSON(500, gin.H{
			"error": err.Error(),
		})
	}

	c.JSON(200, gin.H{
		"message": "account logged out",
	})
}

func (h *AccountHandler) UploadAvatar(c *gin.Context) {
	accountID, err := getAccountID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	f, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "miss file",
		})
	}
	//左移运算：10*2^20=10Mb
	const maxSize = 10 << 20

	if f.Size <= 0 || f.Size > maxSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file size"})
		return
	}

	//检查上传图片的格式
	ext := strings.ToLower(filepath.Ext(f.Filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "only .jpg/.jpeg/.png/.webp allowed",
		})
		return
	}

	dir := filepath.Join(
		".run",
		"uploads",
		"avatars",
		strconv.FormatUint(uint64(accountID), 10),
	)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	//生成随机文件名：1保护隐私，2避免通过猜测文件名访问其他文件
	filename, err := randHex(16)
	filename = filename + ext

	absPath := filepath.Join(dir, filename)

	if err := c.SaveUploadedFile(f, absPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	//配置静态文件路由，将url路径映射到服务器磁盘目录
	urlPath := path.Join("/static", "avatars", strconv.FormatUint(uint64(accountID), 10), filename)
	avatarURL := buildAbsoluteURL(c, urlPath)
	if err := h.accountService.UpdateAvatar(c.Request.Context(), accountID, avatarURL); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"avatar_url": avatarURL})
}

func (h *AccountHandler) UpdateProfile(c *gin.Context) {
	accountID, err := getAccountID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": err.Error(),
		})
		return
	}

	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{
			"error": err.Error(),
		})
		return
	}

	if err := h.accountService.UpdateProfile(c.Request.Context(), accountID, &req); err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{
			"error": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "profile updated",
	})
}

func (h *AccountHandler) Refresh(c *gin.Context) {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(apierror.ClassifyHTTPStatus(err), gin.H{"error": err.Error()})
		return
	}
	newToken, accountID, username, err := h.accountService.RefreshAccessToken(c.Request.Context(), req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid refresh token"})
		return
	}
	c.JSON(http.StatusOK, LoginResponse{
		Token:     newToken,
		AccountID: accountID,
		Username:  username,
	})
}

func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand.Read: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// 判断用户的请求方式
func buildAbsoluteURL(c *gin.Context, p string) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if xf := c.GetHeader("X-Forwarded-Proto"); xf != "" {
		scheme = xf
	}
	return fmt.Sprintf("%s://%s%s", scheme, c.Request.Host, p)
}

func getAccountID(c *gin.Context) (accountid uint, err error) {
	value, exists := c.Get("accountID")
	if !exists {
		return 0, errors.New("accountID not found")
	}
	id, ok := value.(uint)
	if !ok {
		return 0, errors.New("accountID has invalid type")
	}
	return id, nil
}
