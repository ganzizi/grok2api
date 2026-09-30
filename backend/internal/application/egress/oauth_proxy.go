package egress

import (
	"context"
	"errors"
	"fmt"
	"strings"

	domain "github.com/chenyme/grok2api/backend/internal/domain/egress"
	"github.com/chenyme/grok2api/backend/internal/repository"
)

type BuildOAuthProxyInput struct {
	Name     string
	ProxyURL *string
	Enabled  *bool
}

func (s *Service) ListBuildOAuthProxies(ctx context.Context) ([]domain.PublicBuildOAuthProxy, error) {
	if s.oauthProxies == nil {
		return nil, ErrBuildOAuthProxyUnavailable
	}
	values, err := s.oauthProxies.ListBuildOAuthProxies(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]domain.PublicBuildOAuthProxy, 0, len(values))
	for _, value := range values {
		result = append(result, s.publicBuildOAuthProxy(value))
	}
	return result, nil
}

func (s *Service) CreateBuildOAuthProxy(ctx context.Context, input BuildOAuthProxyInput) (domain.PublicBuildOAuthProxy, error) {
	if s.oauthProxies == nil {
		return domain.PublicBuildOAuthProxy{}, ErrBuildOAuthProxyUnavailable
	}
	name, proxyURL, err := validateBuildOAuthProxyInput(input, true)
	if err != nil {
		return domain.PublicBuildOAuthProxy{}, err
	}
	encrypted, err := s.cipher.Encrypt(proxyURL)
	if err != nil {
		return domain.PublicBuildOAuthProxy{}, err
	}
	created, err := s.oauthProxies.CreateBuildOAuthProxy(ctx, domain.BuildOAuthProxy{
		Name: name, EncryptedProxyURL: encrypted, Enabled: input.Enabled == nil || *input.Enabled,
	})
	if err != nil {
		return domain.PublicBuildOAuthProxy{}, err
	}
	return s.publicBuildOAuthProxy(created), nil
}

func (s *Service) UpdateBuildOAuthProxy(ctx context.Context, id uint64, input BuildOAuthProxyInput) (domain.PublicBuildOAuthProxy, error) {
	if s.oauthProxies == nil {
		return domain.PublicBuildOAuthProxy{}, ErrBuildOAuthProxyUnavailable
	}
	current, err := s.oauthProxies.GetBuildOAuthProxy(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.PublicBuildOAuthProxy{}, ErrBuildOAuthProxyNotFound
	}
	if err != nil {
		return domain.PublicBuildOAuthProxy{}, err
	}
	name, proxyURL, err := validateBuildOAuthProxyInput(input, false)
	if err != nil {
		return domain.PublicBuildOAuthProxy{}, err
	}
	current.Name = name
	if input.ProxyURL != nil {
		current.EncryptedProxyURL, err = s.cipher.Encrypt(proxyURL)
		if err != nil {
			return domain.PublicBuildOAuthProxy{}, err
		}
	}
	if input.Enabled != nil {
		current.Enabled = *input.Enabled
	}
	updated, err := s.oauthProxies.UpdateBuildOAuthProxy(ctx, current)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.PublicBuildOAuthProxy{}, ErrBuildOAuthProxyNotFound
	}
	if err != nil {
		return domain.PublicBuildOAuthProxy{}, err
	}
	return s.publicBuildOAuthProxy(updated), nil
}

func (s *Service) DeleteBuildOAuthProxy(ctx context.Context, id uint64) error {
	if s.oauthProxies == nil {
		return ErrBuildOAuthProxyUnavailable
	}
	err := s.oauthProxies.DeleteBuildOAuthProxy(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrBuildOAuthProxyNotFound
	}
	return err
}

func (s *Service) BuildOAuthProxyURL(ctx context.Context, id uint64) (string, error) {
	if s.oauthProxies == nil {
		return "", ErrBuildOAuthProxyUnavailable
	}
	value, err := s.oauthProxies.GetBuildOAuthProxy(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return "", ErrBuildOAuthProxyNotFound
	}
	if err != nil {
		return "", err
	}
	proxyURL, err := s.cipher.Decrypt(value.EncryptedProxyURL)
	if err != nil {
		return "", err
	}
	return NormalizeProxyURL(proxyURL)
}

func validateBuildOAuthProxyInput(input BuildOAuthProxyInput, create bool) (string, string, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 160 {
		return "", "", fmt.Errorf("%w: Build OAuth 代理名称必须在 1 到 160 个字符之间", ErrInvalidInput)
	}
	if input.ProxyURL == nil {
		if create {
			return "", "", fmt.Errorf("%w: 代理地址必填", ErrInvalidInput)
		}
		return name, "", nil
	}
	proxyURL, err := NormalizeProxyURL(*input.ProxyURL)
	if err != nil || proxyURL == "" {
		if err == nil {
			err = errors.New("代理地址不能为空")
		}
		return "", "", fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return name, proxyURL, nil
}

func (s *Service) publicBuildOAuthProxy(value domain.BuildOAuthProxy) domain.PublicBuildOAuthProxy {
	status := value.ProbeStatus
	if status == "" {
		status = domain.ProbeStatusUnknown
	}
	result := domain.PublicBuildOAuthProxy{
		ID: value.ID, Name: value.Name, Enabled: value.Enabled,
		ProbeStatus: status, LastProbedAt: value.LastProbedAt,
		ProbeLatencyMS: value.ProbeLatencyMS, ProbeStatusCode: value.ProbeStatusCode,
		ProbeError: value.ProbeError, ProbeExitIP: value.ProbeExitIP,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
	display, fingerprint, accountBound := s.proxyMetadata(value.EncryptedProxyURL)
	result.ProxyDisplay = display
	result.ProxyFingerprint = fingerprint
	result.AccountBoundProxy = accountBound
	return result
}

func (s *Service) TestBuildOAuthProxyURL(ctx context.Context, proxyURL string) (domain.BuildOAuthProbeResult, error) {
	normalized, err := NormalizeProxyURL(strings.TrimSpace(proxyURL))
	if err != nil || normalized == "" {
		if err == nil {
			err = errors.New("代理地址不能为空")
		}
		return domain.BuildOAuthProbeResult{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return s.probeBuildOAuthURL(ctx, normalized)
}

func (s *Service) TestBuildOAuthProxy(ctx context.Context, id uint64) (domain.BuildOAuthProbeResult, error) {
	if s.oauthProxies == nil {
		return domain.BuildOAuthProbeResult{}, ErrBuildOAuthProxyUnavailable
	}
	value, err := s.oauthProxies.GetBuildOAuthProxy(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.BuildOAuthProbeResult{}, ErrBuildOAuthProxyNotFound
	}
	if err != nil {
		return domain.BuildOAuthProbeResult{}, err
	}
	proxyURL, err := s.cipher.Decrypt(value.EncryptedProxyURL)
	if err != nil {
		return domain.BuildOAuthProbeResult{}, err
	}
	proxyURL, err = NormalizeProxyURL(proxyURL)
	if err != nil || proxyURL == "" {
		if err == nil {
			err = errors.New("代理地址不能为空")
		}
		return domain.BuildOAuthProbeResult{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	result, err := s.probeBuildOAuthURL(ctx, proxyURL)
	if err != nil {
		return result, err
	}
	testedAt := result.TestedAt
	value.ProbeStatus = result.Status
	value.LastProbedAt = &testedAt
	value.ProbeLatencyMS = result.LatencyMS
	value.ProbeStatusCode = result.StatusCode
	value.ProbeError = truncateProbeError(result.Error)
	value.ProbeExitIP = result.ExitIP
	if _, err := s.oauthProxies.UpdateBuildOAuthProxy(ctx, value); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return result, ErrBuildOAuthProxyNotFound
		}
		return result, err
	}
	return result, nil
}

func (s *Service) probeBuildOAuthURL(ctx context.Context, proxyURL string) (domain.BuildOAuthProbeResult, error) {
	s.mu.RLock()
	prober := s.oauthProber
	s.mu.RUnlock()
	if prober == nil {
		return domain.BuildOAuthProbeResult{}, ErrBuildOAuthProxyUnavailable
	}
	return prober(ctx, strings.ReplaceAll(proxyURL, ProxyAccountPlaceholder, "probe")), nil
}

func truncateProbeError(value string) string {
	if len(value) <= 512 {
		return value
	}
	return value[:512]
}
