package models

import (
	"time"
)

// Employee represents the structure of an employee record
type Employee struct {
	EmployeeID           string                `bson:"employeeId" json:"employeeId"`
	PreferredName        string                `bson:"preferredName,omitempty" json:"preferredName,omitempty"`
	FirstName            string                `bson:"firstName" json:"firstName"`
	MiddleName           string                `bson:"middleName,omitempty" json:"middleName,omitempty"`
	LastName             string                `bson:"lastName" json:"lastName"`
	Suffix               string                `bson:"suffix,omitempty" json:"suffix,omitempty"`
	Email                string                `bson:"email" json:"email"`
	Phone                string                `bson:"phone,omitempty" json:"phone,omitempty"`
	SocialSecurityNumber string                `bson:"socialSecurityNumber" json:"socialSecurityNumber"`
	PersonalDetails      PersonalDetails       `bson:"personalDetails" json:"personalDetails"`
	JobHistory           []JobHistory          `bson:"jobHistory" json:"jobHistory"`
	StatusHistory        []StatusHistory       `bson:"statusHistory" json:"statusHistory"`
	CompensationDetails  []CompensationDetails `bson:"compensationDetails" json:"compensationDetails"`
}

type PersonalDetails struct {
	PreferredGender   string             `bson:"preferredGender,omitempty" json:"preferredGender,omitempty"`
	DateOfBirth       time.Time          `bson:"dateOfBirth" json:"dateOfBirth"`
	Gender            string             `bson:"gender" json:"gender"`
	MaritalStatus     string             `bson:"maritalStatus" json:"maritalStatus"`
	Nationality       string             `bson:"nationality,omitempty" json:"nationality,omitempty"`
	PlaceOfBirth      string             `bson:"placeOfBirth,omitempty" json:"placeOfBirth,omitempty"`
	Address           Address            `bson:"address" json:"address"`
	EmergencyContacts []EmergencyContact `bson:"emergencyContacts" json:"emergencyContacts"`
}

type Address struct {
	Street  string `bson:"street" json:"street"`
	City    string `bson:"city" json:"city"`
	State   string `bson:"state" json:"state"`
	ZipCode string `bson:"zipCode" json:"zipCode"`
	Country string `bson:"country" json:"country"`
}

type EmergencyContact struct {
	Name     string   `bson:"name" json:"name"`
	Relation string   `bson:"relation" json:"relation"`
	Phone    string   `bson:"phone" json:"phone"`
	Email    string   `bson:"email,omitempty" json:"email,omitempty"`
	Address  *Address `bson:"address,omitempty" json:"address,omitempty"`
}

type JobHistory struct {
	JobID            string     `bson:"jobId" json:"jobId"`
	Title            string     `bson:"title" json:"title"`
	Department       string     `bson:"department" json:"department"`
	StartDate        time.Time  `bson:"startDate" json:"startDate"`
	EndDate          *time.Time `bson:"endDate,omitempty" json:"endDate,omitempty"`
	Location         string     `bson:"location" json:"location"`
	EmploymentType   string     `bson:"employmentType" json:"employmentType"`
	Manager          *Manager   `bson:"manager,omitempty" json:"manager,omitempty"`
	Responsibilities []string   `bson:"responsibilities,omitempty" json:"responsibilities,omitempty"`
}

type Manager struct {
	Name       string `bson:"name,omitempty" json:"name,omitempty"`
	EmployeeID string `bson:"employeeId,omitempty" json:"employeeId,omitempty"`
	Email      string `bson:"email,omitempty" json:"email,omitempty"`
}

type StatusHistory struct {
	Status string    `bson:"status" json:"status"`
	Date   time.Time `bson:"date" json:"date"`
	Reason string    `bson:"reason,omitempty" json:"reason,omitempty"`
}

type CompensationDetails struct {
	EffectiveDate time.Time   `bson:"effectiveDate" json:"effectiveDate"`
	Salary        float64     `bson:"salary" json:"salary"`
	Currency      string      `bson:"currency" json:"currency"`
	PayFrequency  string      `bson:"payFrequency" json:"payFrequency"`
	Bonuses       int         `bson:"bonuses" json:"bonuses"`
	Allowances    []Allowance `bson:"allowances" json:"allowances"`
}

type Allowance struct {
	Type   string  `bson:"type" json:"type"`
	Amount float64 `bson:"amount" json:"amount"`
}
