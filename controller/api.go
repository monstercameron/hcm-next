package controller

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"hcmnext/database"
	"hcmnext/models"
)

// API struct holds dependencies for the API handlers
type API struct {
	DB *database.Database
}

// NewAPI creates a new instance of API
func NewAPI(db *database.Database) *API {
	return &API{DB: db}
}

// CreateEmployee handles the creation of a new employee
func (api *API) CreateEmployee(w http.ResponseWriter, r *http.Request) {
	var emp models.Employee
	if err := json.NewDecoder(r.Body).Decode(&emp); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	result, err := api.DB.InsertOne("Employee", emp)
	if err != nil {
		http.Error(w, "Failed to create employee", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(result)
}

// GetEmployees retrieves all employees
func (api *API) GetEmployees(w http.ResponseWriter, r *http.Request) {
	log.Println("GetEmployees: Start retrieving employees")

	filter := bson.M{}
	cursor, err := api.DB.FindMany("Employee", filter)
	if err != nil {
		log.Printf("GetEmployees: Error retrieving employees from database: %v", err)
		http.Error(w, "Failed to retrieve employees", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(r.Context())

	var employees []models.Employee
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

// GetEmployee retrieves a single employee by ID
func (api *API) GetEmployee(w http.ResponseWriter, r *http.Request) {
	employeeID := r.PathValue("id")

	var emp models.Employee
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(emp); err != nil {
		log.Printf("GetEmployee: Error encoding response to JSON: %v", err)
		http.Error(w, "Failed to encode employee as JSON", http.StatusInternalServerError)
	}
}

// UpdateEmployee updates an existing employee
func (api *API) UpdateEmployee(w http.ResponseWriter, r *http.Request) {
	employeeID := r.PathValue("id")

	var emp models.Employee
	if err := json.NewDecoder(r.Body).Decode(&emp); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if emp.EmployeeID != employeeID {
		http.Error(w, "ID in URL does not match ID in request body", http.StatusBadRequest)
		return
	}

	filter := bson.M{"employeeId": employeeID}
	update := bson.M{"$set": emp}

	result, err := api.DB.UpdateOne("Employee", filter, update)
	if err != nil {
		http.Error(w, "Failed to update employee", http.StatusInternalServerError)
		return
	}

	if result.ModifiedCount == 0 {
		http.Error(w, "Employee not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Employee updated successfully")
}

// DeleteEmployee removes an employee from the database
func (api *API) DeleteEmployee(w http.ResponseWriter, r *http.Request) {
	employeeID := r.PathValue("id")

	filter := bson.M{"employeeId": employeeID}
	result, err := api.DB.DeleteOne("Employee", filter)
	if err != nil {
		http.Error(w, "Failed to delete employee", http.StatusInternalServerError)
		return
	}

	if result.DeletedCount == 0 {
		http.Error(w, "Employee not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Employee deleted successfully")
}
