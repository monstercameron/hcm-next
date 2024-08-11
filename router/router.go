package router

import (
	"net/http"

	"hcmnext/controller"
)

type Router struct {
	controller     *controller.Controller
	homeController *controller.HomeController
	employeeAPI    *controller.API
	testController *controller.TestController
}

func NewRouter(ctrl *controller.Controller, homeCtrl *controller.HomeController, empAPI *controller.API, testAPI *controller.TestController) *Router {
	return &Router{
		controller:     ctrl,
		homeController: homeCtrl,
		employeeAPI:    empAPI,
		testController: testAPI,
	}
}

func (r *Router) SetupRoutes() {
	// Existing routes
	http.HandleFunc("/", r.homeController.ServeHome)
	http.HandleFunc("/ws", r.controller.HandleWebSocket)

	// Employee API routes
	http.HandleFunc("POST /api/employees", r.employeeAPI.CreateEmployee)
	http.HandleFunc("GET /api/employees", r.employeeAPI.GetEmployees)
	http.HandleFunc("GET /api/employees/{id}", r.employeeAPI.GetEmployee)
	http.HandleFunc("PUT /api/employees/{id}", r.employeeAPI.UpdateEmployee)
	http.HandleFunc("DELETE /api/employees/{id}", r.employeeAPI.DeleteEmployee)

	// test routes
	http.HandleFunc("GET /api/exectionplan", r.testController.HandleGenerateExecutionPlan)
	http.HandleFunc("GET /api/usetool", r.testController.HandleToolUse)
	http.HandleFunc("GET /api/math", r.testController.HandleGeneratemath)
}