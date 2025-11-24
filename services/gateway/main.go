package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	gv1 "godrive/proto/godrive/v1"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
)

type deps struct {
	auth    gv1.AuthServiceClient
	files   gv1.FilesServiceClient
	storage gv1.StorageServiceClient
}

func main() {
	authConn, _ := grpc.Dial(env("AUTH_ADDR", "auth:50051"), grpc.WithInsecure())
	filesConn, _ := grpc.Dial(env("FILES_ADDR", "files:50052"), grpc.WithInsecure())
	storageConn, _ := grpc.Dial(env("STORAGE_ADDR", "storage:50053"), grpc.WithInsecure())

	d := &deps{
		auth:    gv1.NewAuthServiceClient(authConn),
		files:   gv1.NewFilesServiceClient(filesConn),
		storage: gv1.NewStorageServiceClient(storageConn),
	}

	r := gin.Default()

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true, "time": time.Now().UTC()})
	})

	r.POST("/signup", d.signup)
	r.POST("/login", d.login)

	auth := r.Group("/", d.authz)
	{
		auth.GET("/files", d.listFiles)
		auth.POST("/files/upload-intent", d.createUploadIntent)
		auth.GET("/files/:id/download", d.downloadURL)
		auth.DELETE("/files/:id", d.deleteFile)
	}

	port := env("PORT", "8080")
	log.Println("gateway on :" + port)

	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}

func (d *deps) signup(c *gin.Context) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := c.BindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad payload"})
		return
	}

	u, err := d.auth.SignUp(c, &gv1.Credentials{Email: in.Email, Password: in.Password})
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "email exists"})
		return
	}

	c.JSON(http.StatusCreated, u)
}

func (d *deps) login(c *gin.Context) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := c.BindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad payload"})
		return
	}

	t, err := d.auth.Login(c, &gv1.Credentials{Email: in.Email, Password: in.Password})
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": t.AccessToken, "expires_at": t.ExpiresAt})
}

func (d *deps) authz(c *gin.Context) {
	ah := c.GetHeader("Authorization")
	if !strings.HasPrefix(ah, "Bearer ") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
		return
	}

	tok := strings.TrimPrefix(ah, "Bearer ")

	u, err := d.auth.Verify(c, &gv1.Token{AccessToken: tok})
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	c.Set("uid", u.Id)
	c.Next()
}

func (d *deps) listFiles(c *gin.Context) {
	uid := c.GetInt64("uid")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))

	resp, err := d.files.List(c, &gv1.ListFilesRequest{
		OwnerId:  uid,
		Page:     int32(page),
		PageSize: 20,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list failed"})
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (d *deps) createUploadIntent(c *gin.Context) {
	uid := c.GetInt64("uid")

	var in struct {
		Filename  string `json:"filename"`
		Mime      string `json:"mime"`
		SizeBytes int64  `json:"size_bytes"`
	}

	if err := c.BindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad payload"})
		return
	}

	key := fmt.Sprintf("user/%d/%d_%s", uid, time.Now().UnixNano(), in.Filename)

	p, err := d.storage.PresignUpload(c, &gv1.PresignUploadRequest{
		ObjectKey: key,
		Mime:      in.Mime,
		SizeBytes: in.SizeBytes,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "presign failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"upload_url": p.Url,
		"object_key": key,
		"headers":    p.Headers,
		"expires_at": p.ExpiresAt,
	})
}

func (d *deps) downloadURL(c *gin.Context) {
	uid := c.GetInt64("uid")
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)

	resp, err := d.files.GetDownloadURL(c, &gv1.DownloadURLRequest{
		OwnerId: uid,
		FileId:  id,
	})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"url": resp.DownloadUrl, "expires_at": resp.ExpiresAt})
}

func (d *deps) deleteFile(c *gin.Context) {
	uid := c.GetInt64("uid")
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)

	resp, err := d.files.Delete(c, &gv1.DeleteFileRequest{OwnerId: uid, FileId: id})
	if err != nil || !resp.Ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"deleted": id})
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
