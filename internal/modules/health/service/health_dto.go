// File: internal/modules/health/service/health_dto.go

package service

// HealthResponseDTO is what a probe or a dashboard receives.
//
// Deliberately thin. These endpoints are unauthenticated, so everything here is
// public: no version string, no hostname, no dependency addresses, no error
// text. A health endpoint is a common source of quiet information disclosure —
// it is the one URL people expose without thinking about who can read it.
type HealthResponseDTO struct {
	Status string              `json:"status"`
	Checks []*CheckResponseDTO `json:"checks,omitempty"`
	Uptime string              `json:"uptime"`
}

// CheckResponseDTO is one dependency's result.
type CheckResponseDTO struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Duration string `json:"duration"`
}

// ToResponse maps a report onto the wire shape.
func ToResponse(report *Report) *HealthResponseDTO {
	if report == nil {
		return &HealthResponseDTO{Status: StatusFail.String()}
	}

	var response *HealthResponseDTO = &HealthResponseDTO{
		Status: report.Status.String(),
		Checks: make([]*CheckResponseDTO, 0, len(report.Checks)),
		Uptime: report.Uptime.Round(1e9).String(),
	}

	var check Check
	for _, check = range report.Checks {
		response.Checks = append(response.Checks, &CheckResponseDTO{
			Name:     check.Name,
			Status:   check.Status.String(),
			Duration: check.Duration.Round(1e6).String(),
		})
	}
	return response
}
