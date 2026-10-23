package complete

// allPlanners lists the domains in dependency order. Setup runs once (first-day window);
// PlanDay runs for every simulated day, in this order.
func allPlanners() []Planner {
	return []Planner{
		orgPlanner{},
		referencePlanner{},
		pharmacyPlanner{},
		populationPlanner{},
		visitPlanner{},
		agendaPlanner{},
		surgeryPlanner{},
		financePlanner{},
		insurancePlanner{},
		supportPlanner{},
		notificationPlanner{},
	}
}

// Domain states (filled by the planners).
