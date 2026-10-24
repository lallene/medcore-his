package dashboard

type Service interface {
	GetDashboard(filter DashboardFilter) (*DashboardResponse, error)
}

type service struct {
	repository Repository
}

func NewService(repository Repository) Service {
	return &service{repository: repository}
}

func (s *service) GetDashboard(filter DashboardFilter) (*DashboardResponse, error) {
	stats, err := s.repository.GetStats(filter)

	if err != nil {
		return nil, err
	}

	activities, err := s.repository.GetRecentActivities(10, filter)
	if err != nil {
		activities = []ActivityItem{}
	}

	stats.RecentActivities = activities

	return stats, nil
}
