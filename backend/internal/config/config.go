package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

// Config holds all environment variables for the Go audio service.
// Values mirror the Python .env file — shared with the FastAPI process.
type Config struct {
	// Server
	Port     int    `env:"GO_AUDIO_PORT" envDefault:"8001"`
	GRPCAddr string `env:"GRPC_ADDR"     envDefault:"localhost:50051"`

	// Redis
	RedisURL      string `env:"REDIS_URL"      envDefault:"redis://localhost:6379/1"`
	RedisPassword string `env:"REDIS_PASSWORD" envDefault:""`

	// MySQL (Phase 4 REST API)
	MySQLHost     string `env:"MYSQL_HOST"     envDefault:"localhost"`
	MySQLUser     string `env:"MYSQL_USER"     envDefault:"callified"`
	MySQLPassword string `env:"MYSQL_PASSWORD" envDefault:""`
	MySQLDatabase string `env:"MYSQL_DATABASE" envDefault:"callified_ai"`

	// JWT auth (shared secret with Python FastAPI)
	JWTSecret string `env:"JWT_SECRET_KEY" envDefault:"your-secret-key-replace-in-production"`

	// LLM providers (Phase 0)
	GeminiAPIKey  string `env:"GEMINI_API_KEY"`
	GeminiModel   string `env:"GEMINI_MODEL"    envDefault:"gemini-2.5-flash"`
	GroqAPIKey    string `env:"GROQ_API_KEY"`
	GroqModel     string `env:"GROQ_MODEL"      envDefault:"llama-3.3-70b-versatile"`
	LLMProvider   string `env:"LLM_PROVIDER"    envDefault:"gemini"`
	RAGServiceURL string `env:"RAG_SERVICE_URL" envDefault:"http://rag-service:8002"`

	// Deepgram
	DeepgramAPIKey string `env:"DEEPGRAM_API_KEY"`

	// TTS providers
	ElevenLabsAPIKey string `env:"ELEVENLABS_API_KEY"`
	SarvamAPIKey     string `env:"SARVAM_API_KEY"`
	SmallestAPIKey   string `env:"SMALLEST_API_KEY"`

	// Recordings directory
	RecordingsDir string `env:"RECORDINGS_DIR" envDefault:"recordings"`

	// Telephony — Phase 2
	TwilioAccountSID string `env:"TWILIO_ACCOUNT_SID"`
	TwilioAuthToken  string `env:"TWILIO_AUTH_TOKEN"`
	TwilioPhone      string `env:"TWILIO_PHONE_NUMBER"`
	ExotelAPIKey     string `env:"EXOTEL_API_KEY"`
	ExotelAPIToken   string `env:"EXOTEL_API_TOKEN"`
	ExotelAccountSID string `env:"EXOTEL_ACCOUNT_SID"`
	ExotelCallerID   string `env:"EXOTEL_CALLER_ID"`
	ExotelAppID      string `env:"EXOTEL_APP_ID"     envDefault:"1210468"`
	DefaultProvider  string `env:"DEFAULT_PROVIDER"  envDefault:"exotel"`
	PublicServerURL  string `env:"PUBLIC_SERVER_URL" envDefault:"http://localhost:8001"`

	// Billing (Phase 3B)
	RazorpayKeyID         string `env:"RAZORPAY_KEY_ID"`
	RazorpayKeySecret     string `env:"RAZORPAY_KEY_SECRET"`
	RazorpayWebhookSecret string `env:"RAZORPAY_WEBHOOK_SECRET"`

	// Email / SMTP (Phase 3B)
	SMTPHost     string `env:"SMTP_HOST"      envDefault:"smtp.gmail.com"`
	SMTPPort     int    `env:"SMTP_PORT"      envDefault:"587"`
	SMTPUser     string `env:"SMTP_USER"`
	SMTPPassword string `env:"SMTP_PASSWORD"`
	SMTPFromName string `env:"SMTP_FROM_NAME" envDefault:"Callified AI"`
	AppURL       string `env:"APP_URL"        envDefault:"https://test.callified.ai"`

	// WhatsApp (Phase 3C)
	MetaVerifyToken string `env:"META_WHATSAPP_VERIFY_TOKEN"`
}

// DSN returns a MySQL DSN string for database/sql.
func (c *Config) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:3306)/%s?parseTime=true&loc=UTC&charset=utf8mb4",
		c.MySQLUser, c.MySQLPassword, c.MySQLHost, c.MySQLDatabase)
}

// Load parses environment variables into Config.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
