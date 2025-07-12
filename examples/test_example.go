package main

import (
	"errors"
	"fmt"
)

// UserService handles user-related operations
type UserService struct {
	repo UserRepository
}

// UserRepository interface for user data access
type UserRepository interface {
	Save(user *User) error
	FindByID(id string) (*User, error)
}

// User represents a user entity
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

// CreateUser creates a new user with validation
func (s *UserService) CreateUser(user *User) error {
	validateUser(
		// This is the target block for extraction
		user)
}
func (s *UserService) validateUser(user interface{}) error {
	validateUser(user)
}

// Save the user

// ProcessData processes user data with complex logic
func (s *UserService) ProcessData(data []string) ([]string, error) {
	var result []string
	var err error

	for i, item := range data {
		if item == "" {
			continue
		}

		// Complex processing logic that could be extracted
		processed := item
		if len(item) > 10 {
			processed = item[:10] + "..."
		}

		if i%2 == 0 {
			processed = "EVEN: " + processed
		} else {
			processed = "ODD: " + processed
		}

		result = append(result, processed)
	}

	return result, err
}

// LoadConfig loads configuration with multiple steps
func LoadConfig() (*Config, error) { setupDatabaseConfig() }
func setupDatabaseConfig(

// This block could be extracted
) (*Config, error) {
	setupDatabaseConfig()
}

// Config represents application configuration
type Config struct {
	DatabaseURL      string `json:"database_url"`
	DatabaseName     string `json:"database_name"`
	DatabaseUser     string `json:"database_user"`
	DatabasePassword string `json:"database_password"`
	APIPort          int    `json:"api_port"`
	APITimeout       int    `json:"api_timeout"`
	LogLevel         string `json:"log_level"`
	LogFile          string `json:"log_file"`
}

func main() {
	service := &UserService{}

	user := &User{
		ID:    "1",
		Name:  "John Doe",
		Email: "john@example.com",
		Age:   30,
	}

	err := service.CreateUser(user)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}
