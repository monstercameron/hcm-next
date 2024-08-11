package controller

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"hcmnext/database"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// Employee represents the structure of an employee record
type Employee struct {
	ID        int    `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
}

// API struct holds dependencies for the API handlers
type API struct {
	DB *database.Database
}

// NewAPI creates a new instance of API
func NewAPI(db *database.Database) *API {
	return &API{DB: db}
}

// createEmployee handles the creation of a new employee
func (api *API) CreateEmployee(w http.ResponseWriter, r *http.Request) {
	// Parse the request body
	var emp Employee
	err := json.NewDecoder(r.Body).Decode(&emp)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Insert the employee into the database
	result, err := api.DB.InsertOne("employees", emp)
	if err != nil {
		http.Error(w, "Failed to create employee", http.StatusInternalServerError)
		return
	}

	// Respond with the created employee
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(result)
}

// getEmployees retrieves all employees
func (api *API) GetEmployees(w http.ResponseWriter, r *http.Request) {
	log.Println("GetEmployees: Start retrieving employees")

	// Use an empty filter to retrieve all documents
	filter := bson.M{}

	// Retrieve employees from the database
	cursor, err := api.DB.FindMany("Employee", filter)
	if err != nil {
		log.Printf("GetEmployees: Error retrieving employees from database: %v", err)
		http.Error(w, "Failed to retrieve employees", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(r.Context())

	var employees []Employee
	if err = cursor.All(r.Context(), &employees); err != nil {
		log.Printf("GetEmployees: Error decoding employees: %v", err)
		http.Error(w, "Failed to process employees", http.StatusInternalServerError)
		return
	}

	log.Printf("GetEmployees: Retrieved %d employees", len(employees))

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(employees); err != nil {
		log.Printf("GetEmployees: Error encoding response to JSON: %v", err)
		http.Error(w, "Failed to encode employees as JSON", http.StatusInternalServerError)
	}
}

// getEmployee retrieves a single employee by ID
func (api *API) GetEmployee(w http.ResponseWriter, r *http.Request) {
	// Extract the employee ID from the URL
	employeeID := r.PathValue("id")

	// Retrieve the employee from the database
	var emp Employee
	filter := bson.M{"employeeId": employeeID}
	err := api.DB.FindOne("Employee", filter, &emp)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			http.Error(w, "Employee not found", http.StatusNotFound)
		} else {
			log.Printf("GetEmployee: Error retrieving employee: %v", err)
			http.Error(w, "Failed to retrieve employee", http.StatusInternalServerError)
		}
		return
	}

	// Respond with the employee data
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(emp); err != nil {
		log.Printf("GetEmployee: Error encoding response to JSON: %v", err)
		http.Error(w, "Failed to encode employee as JSON", http.StatusInternalServerError)
	}
}

// updateEmployee updates an existing employee
func (api *API) UpdateEmployee(w http.ResponseWriter, r *http.Request) {
	// Extract the employee ID from the URL
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid employee ID", http.StatusBadRequest)
		return
	}

	// Parse the request body
	var emp Employee
	err = json.NewDecoder(r.Body).Decode(&emp)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Ensure the ID in the URL matches the ID in the request body
	if emp.ID != id {
		http.Error(w, "ID in URL does not match ID in request body", http.StatusBadRequest)
		return
	}

	// Create an update document
	update := map[string]interface{}{
		"$set": map[string]interface{}{
			"firstName": emp.FirstName,
			"lastName":  emp.LastName,
			"email":     emp.Email,
		},
	}

	// Update the employee in the database
	result, err := api.DB.UpdateOne("employees", map[string]interface{}{"id": id}, update)
	if err != nil {
		http.Error(w, "Failed to update employee", http.StatusInternalServerError)
		return
	}

	// Check if the employee was found and updated
	if result.ModifiedCount == 0 {
		http.Error(w, "Employee not found", http.StatusNotFound)
		return
	}

	// Respond with a success message
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Employee updated successfully")
}

// deleteEmployee removes an employee from the database
func (api *API) DeleteEmployee(w http.ResponseWriter, r *http.Request) {
	// Extract the employee ID from the URL
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid employee ID", http.StatusBadRequest)
		return
	}

	// Delete the employee from the database
	result, err := api.DB.DeleteOne("employees", map[string]interface{}{"id": id})
	if err != nil {
		http.Error(w, "Failed to delete employee", http.StatusInternalServerError)
		return
	}

	// Check if the employee was found and deleted
	if result.DeletedCount == 0 {
		http.Error(w, "Employee not found", http.StatusNotFound)
		return
	}

	// Respond with a success message
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Employee deleted successfully")
}
