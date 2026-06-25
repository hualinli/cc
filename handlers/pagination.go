package handlers

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// parsePaginationParams 解析分页参数
// 返回 page 和 pageSize，默认值分别为 1 和 20
// page 最小为 1，pageSize 最小为 1，最大为 100
func parsePaginationParams(c *gin.Context) (page int, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ = strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	return page, pageSize
}
