package dashboard

type Service interface {
	GetDashboard() (*DashboardResponse, error)
}

type service struct {
	repository Repository
}

func NewService(repository Repository) Service {
	return &service{repository: repository}
}

func (s *service) GetDashboard() (*DashboardResponse, error) {
	stats, err := s.repository.GetStats()

	if err != nil {
		return nil, err
	}

	activities, err := s.repository.GetRecentActivities(10)

	if err != nil {
		return nil, err
	}

	stats.RecentActivities = activities

	return stats, nil
}
