package router

import (
	"net/http"

	"hcmnext/controller"
)

type Router struct {
	controller *controller.Controller
	homeController *controller.HomeController
}

func NewRouter(ctrl *controller.Controller, homeCtrl *controller.HomeController) *Router {
	return &Router{
		controller: ctrl,
		homeController: homeCtrl,
	}
}

func (r *Router) SetupRoutes() {
	http.HandleFunc("/", r.homeController.ServeHome)
	http.HandleFunc("/ws", r.controller.HandleWebSocket)
}