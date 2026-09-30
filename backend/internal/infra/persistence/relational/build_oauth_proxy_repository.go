package relational

import (
	"context"
	"strings"

	"github.com/chenyme/grok2api/backend/internal/domain/egress"
)

func (r *EgressRepository) ListBuildOAuthProxies(ctx context.Context) ([]egress.BuildOAuthProxy, error) {
	var rows []buildOAuthProxyModel
	err := r.db.db.WithContext(ctx).
		Model(&buildOAuthProxyModel{}).
		Order("LOWER(name) ASC, id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	values := make([]egress.BuildOAuthProxy, 0, len(rows))
	for _, row := range rows {
		values = append(values, toBuildOAuthProxyDomain(row))
	}
	return values, nil
}

func (r *EgressRepository) GetBuildOAuthProxy(ctx context.Context, id uint64) (egress.BuildOAuthProxy, error) {
	var row buildOAuthProxyModel
	if err := r.db.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return egress.BuildOAuthProxy{}, mapError(err)
	}
	return toBuildOAuthProxyDomain(row), nil
}

func (r *EgressRepository) CreateBuildOAuthProxy(ctx context.Context, value egress.BuildOAuthProxy) (egress.BuildOAuthProxy, error) {
	row := fromBuildOAuthProxyDomain(value)
	if err := r.db.db.WithContext(ctx).Create(&row).Error; err != nil {
		return egress.BuildOAuthProxy{}, mapError(err)
	}
	return toBuildOAuthProxyDomain(row), nil
}

func (r *EgressRepository) UpdateBuildOAuthProxy(ctx context.Context, value egress.BuildOAuthProxy) (egress.BuildOAuthProxy, error) {
	var current buildOAuthProxyModel
	if err := r.db.db.WithContext(ctx).First(&current, value.ID).Error; err != nil {
		return egress.BuildOAuthProxy{}, mapError(err)
	}
	current.Name = strings.TrimSpace(value.Name)
	current.EncryptedProxyURL = value.EncryptedProxyURL
	current.Enabled = value.Enabled
	if value.ProbeStatus != "" {
		current.ProbeStatus = string(value.ProbeStatus)
		current.LastProbedAt = value.LastProbedAt
		current.ProbeLatencyMS = value.ProbeLatencyMS
		current.ProbeStatusCode = value.ProbeStatusCode
		current.ProbeError = value.ProbeError
	}
	if err := r.db.db.WithContext(ctx).Save(&current).Error; err != nil {
		return egress.BuildOAuthProxy{}, mapError(err)
	}
	return toBuildOAuthProxyDomain(current), nil
}

func (r *EgressRepository) DeleteBuildOAuthProxy(ctx context.Context, id uint64) error {
	var row buildOAuthProxyModel
	if err := r.db.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return mapError(err)
	}
	return mapError(r.db.db.WithContext(ctx).Delete(&row).Error)
}

func toBuildOAuthProxyDomain(row buildOAuthProxyModel) egress.BuildOAuthProxy {
	return egress.BuildOAuthProxy{
		ID: row.ID, Name: row.Name, EncryptedProxyURL: row.EncryptedProxyURL,
		Enabled: row.Enabled, ProbeStatus: egress.ProbeStatus(row.ProbeStatus),
		LastProbedAt: row.LastProbedAt, ProbeLatencyMS: row.ProbeLatencyMS,
		ProbeStatusCode: row.ProbeStatusCode, ProbeError: row.ProbeError,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func fromBuildOAuthProxyDomain(value egress.BuildOAuthProxy) buildOAuthProxyModel {
	return buildOAuthProxyModel{
		ID: value.ID, Name: value.Name, EncryptedProxyURL: value.EncryptedProxyURL,
		Enabled: value.Enabled, ProbeStatus: probeStatusOrUnknown(value.ProbeStatus),
		LastProbedAt: value.LastProbedAt, ProbeLatencyMS: value.ProbeLatencyMS,
		ProbeStatusCode: value.ProbeStatusCode, ProbeError: value.ProbeError,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func probeStatusOrUnknown(value egress.ProbeStatus) string {
	if value == "" {
		return string(egress.ProbeStatusUnknown)
	}
	return string(value)
}
