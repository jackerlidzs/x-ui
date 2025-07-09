package middleware

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

var validate *validator.Validate

func init() {
	validate = validator.New()
	
	// Custom validators
	validate.RegisterValidation("protocol", validateProtocol)
	validate.RegisterValidation("port", validatePort)
	validate.RegisterValidation("email_format", validateEmailFormat)
}

// InboundValidator for inbound validation
type InboundValidator struct {
	Port           int    `json:"port" binding:"required,min=1,max=65535" validate:"port"`
	Protocol       string `json:"protocol" binding:"required" validate:"protocol"`
	Remark         string `json:"remark" binding:"required,min=1,max=100"`
	Listen         string `json:"listen"`
	Settings       string `json:"settings" binding:"required"`
	StreamSettings string `json:"streamSettings"`
	Sniffing       string `json:"sniffing"`
	Enable         *bool  `json:"enable"`
}

// UserValidator for user validation
type UserValidator struct {
	Username string `json:"username" binding:"required,min=3,max=50,alphanum"`
	Password string `json:"password" binding:"required,min=6,max=100"`
}

// SettingValidator for settings validation
type SettingValidator struct {
	Key   string `json:"key" binding:"required,min=1,max=100"`
	Value string `json:"value" binding:"required"`
}

// ClientValidator for client validation
type ClientValidator struct {
	Email      string `json:"email" binding:"required" validate:"email_format"`
	LimitIP    int    `json:"limitIp" binding:"min=0,max=1000"`
	TotalGB    int64  `json:"totalGB" binding:"min=0"`
	ExpiryTime int64  `json:"expiryTime" binding:"min=0"`
}

// Custom validation functions
func validateProtocol(fl validator.FieldLevel) bool {
	protocol := fl.Field().String()
	validProtocols := []string{"vmess", "vless", "trojan", "shadowsocks", "dokodemo-door", "http", "socks"}
	
	for _, valid := range validProtocols {
		if protocol == valid {
			return true
		}
	}
	return false
}

func validatePort(fl validator.FieldLevel) bool {
	port := int(fl.Field().Int())
	
	// Common system ports that should be avoided
	reservedPorts := []int{22, 80, 443, 53, 21, 25, 110, 143, 993, 995}
	
	for _, reserved := range reservedPorts {
		if port == reserved {
			return false
		}
	}
	
	return port >= 1024 && port <= 65535
}

func validateEmailFormat(fl validator.FieldLevel) bool {
	email := fl.Field().String()
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	return emailRegex.MatchString(email)
}

// ValidationMiddleware returns a gin middleware for request validation
func ValidationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Add custom headers for validation responses
		c.Header("X-Validation-Version", "1.0")
		c.Next()
	}
}

// ValidateInbound validates inbound data
func ValidateInbound(c *gin.Context) (*InboundValidator, error) {
	var validator InboundValidator
	
	if err := c.ShouldBindJSON(&validator); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "VALIDATION_ERROR",
				"message": "Invalid input data",
				"details": parseValidationErrors(err),
			},
		})
		return nil, err
	}
	
	// Additional custom validation
	if err := validate.Struct(&validator); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "VALIDATION_ERROR",
				"message": "Validation failed",
				"details": parseValidationErrors(err),
			},
		})
		return nil, err
	}
	
	return &validator, nil
}

// ValidateUser validates user data
func ValidateUser(c *gin.Context) (*UserValidator, error) {
	var validator UserValidator
	
	if err := c.ShouldBindJSON(&validator); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "VALIDATION_ERROR",
				"message": "Invalid user data",
				"details": parseValidationErrors(err),
			},
		})
		return nil, err
	}
	
	return &validator, nil
}

// ValidateClient validates client data
func ValidateClient(c *gin.Context) (*ClientValidator, error) {
	var validator ClientValidator
	
	if err := c.ShouldBindJSON(&validator); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "VALIDATION_ERROR",
				"message": "Invalid client data",
				"details": parseValidationErrors(err),
			},
		})
		return nil, err
	}
	
	if err := validate.Struct(&validator); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "VALIDATION_ERROR",
				"message": "Client validation failed",
				"details": parseValidationErrors(err),
			},
		})
		return nil, err
	}
	
	return &validator, nil
}

// parseValidationErrors converts validation errors to user-friendly format
func parseValidationErrors(err error) []gin.H {
	var errors []gin.H
	
	if validationErrors, ok := err.(validator.ValidationErrors); ok {
		for _, e := range validationErrors {
			errors = append(errors, gin.H{
				"field":   strings.ToLower(e.Field()),
				"message": getErrorMessage(e),
				"value":   e.Value(),
			})
		}
	} else {
		errors = append(errors, gin.H{
			"field":   "general",
			"message": err.Error(),
		})
	}
	
	return errors
}

// getErrorMessage returns user-friendly error messages
func getErrorMessage(e validator.FieldError) string {
	switch e.Tag() {
	case "required":
		return "This field is required"
	case "min":
		return "Value is too short (minimum " + e.Param() + ")"
	case "max":
		return "Value is too long (maximum " + e.Param() + ")"
	case "email":
		return "Invalid email format"
	case "alphanum":
		return "Only alphanumeric characters allowed"
	case "protocol":
		return "Invalid protocol (allowed: vmess, vless, trojan, shadowsocks, dokodemo-door, http, socks)"
	case "port":
		return "Invalid port (must be between 1024-65535 and not a reserved port)"
	case "email_format":
		return "Invalid email format"
	default:
		return "Invalid value"
	}
}

// SanitizeInput removes dangerous characters from input
func SanitizeInput(input string) string {
	// Remove SQL injection patterns
	sqlPatterns := []string{
		"'", "\"", ";", "--", "/*", "*/", "xp_", "sp_",
		"SELECT", "INSERT", "UPDATE", "DELETE", "DROP", "CREATE",
		"UNION", "OR", "AND", "script", "<", ">",
	}
	
	result := input
	for _, pattern := range sqlPatterns {
		result = strings.ReplaceAll(result, pattern, "")
		result = strings.ReplaceAll(result, strings.ToLower(pattern), "")
		result = strings.ReplaceAll(result, strings.ToUpper(pattern), "")
	}
	
	return strings.TrimSpace(result)
}

// ValidateIPAddress validates IP address format
func ValidateIPAddress(ip string) bool {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return false
	}
	
	for _, part := range parts {
		num, err := strconv.Atoi(part)
		if err != nil || num < 0 || num > 255 {
			return false
		}
	}
	
	return true
}