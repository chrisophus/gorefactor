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
	ID    string
	Name  string
	Email string
	Age   int
}

// CreateUser creates a new user with validation
func (s *UserService) CreateUser(user *User) error {
	if user == nil {
		return errors.New("user cannot be nil")
	}

	if user.Name == "" {
		return errors.New("name is required")
	}
	if user.Email == "" {
		return errors.New("email is required")
	}
	if user.Age < 0 {
		return errors.New("age must be positive")
	}
	if user.Age > 150 {
		return errors.New("age must be reasonable")
	}

	err := s.repo.Save(user)
	if err != nil {
		return fmt.Errorf("failed to save user: %w", err)
	}

	fmt.Printf("User %s created successfully\n", user.Name)
	return nil
}

// ProcessData processes user data with complex logic
func (s *UserService) ProcessData(data []string) ([]string, error) {
	fmt.Printf("Processing %d data items\n", len(data))

	var result []string

	for i, item := range data {
		if item == "" {
			continue
		}

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

	return result, nil
}
