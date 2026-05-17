package main

import (
	"errors"
	"fmt"
	"strings"
)

// OrderService handles order processing
type OrderService struct{}

// ProcessOrder is a complex function with multiple concerns
func (s *OrderService) ProcessOrder(customerID string, items []Item, shippingAddress string) (OrderResult, error) {
	// Validation block - could be extracted
	if customerID == "" {
		return OrderResult{}, errors.New("customer ID is required")
	}
	if len(items) == 0 {
		return OrderResult{}, errors.New("order must contain at least one item")
	}
	if shippingAddress == "" {
		return OrderResult{}, errors.New("shipping address is required")
	}

	// Load customer and validate credit
	customer, err := loadCustomer(customerID)
	if err != nil {
		return OrderResult{}, fmt.Errorf("failed to load customer: %w", err)
	}

	// Calculate price with complex logic - could be extracted
	totalPrice := 0
	for _, item := range items {
		if item.Price < 0 {
			return OrderResult{}, errors.New("invalid item price")
		}
		totalPrice += item.Price

		if item.Quantity < 1 {
			return OrderResult{}, errors.New("invalid item quantity")
		}
		totalPrice *= item.Quantity
	}

	// Apply discounts - another extractable block
	discountPercent := 0
	if customer.IsVIP {
		discountPercent = 15
	} else if totalPrice > 1000 {
		discountPercent = 10
	} else if totalPrice > 500 {
		discountPercent = 5
	}

	discount := (totalPrice * discountPercent) / 100
	finalPrice := totalPrice - discount

	// Tax calculation - could be extracted
	taxRate := 0.08
	if strings.Contains(shippingAddress, "TX") || strings.Contains(shippingAddress, "CA") {
		taxRate = 0.0625
	}
	tax := int(float64(finalPrice) * taxRate)
	finalPrice += tax

	// Shipping calculation
	shippingCost := 10
	if finalPrice > 500 {
		shippingCost = 0
	} else if finalPrice > 200 {
		shippingCost = 5
	}
	finalPrice += shippingCost

	// Create order and save
	order := OrderResult{
		CustomerID:      customerID,
		Items:           len(items),
		SubTotal:        totalPrice,
		Discount:        discount,
		Tax:             tax,
		Shipping:        shippingCost,
		Total:           finalPrice,
		ShippingAddress: shippingAddress,
	}

	if err := saveOrder(order); err != nil {
		return OrderResult{}, fmt.Errorf("failed to save order: %w", err)
	}

	return order, nil
}

// Item represents a product in an order
type Item struct {
	ID       string
	Price    int
	Quantity int
}

// OrderResult represents the result of processing an order
type OrderResult struct {
	CustomerID      string
	Items           int
	SubTotal        int
	Discount        int
	Tax             int
	Shipping        int
	Total           int
	ShippingAddress string
}

// Customer represents a customer
type Customer struct {
	ID    string
	IsVIP bool
}

func loadCustomer(id string) (Customer, error) {
	return Customer{ID: id, IsVIP: false}, nil
}

func saveOrder(order OrderResult) error {
	return nil
}
func main() {}
