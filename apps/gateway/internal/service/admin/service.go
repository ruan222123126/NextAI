package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/service/ports"
)

type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type Dependencies struct {
	Store             ports.StateStore
	DataDir           string
	SupportedChannels map[string]struct{}
}

type Service struct {
	deps Dependencies
}

type CreateSkillInput struct {
	Name       string
	Content    string
	References map[string]interface{}
	Scripts    map[string]interface{}
}

func NewService(deps Dependencies) *Service {
	normalized := map[string]struct{}{}
	for raw := range deps.SupportedChannels {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		normalized[name] = struct{}{}
	}
	deps.SupportedChannels = normalized
	return &Service{deps: deps}
}

func (s *Service) ListEnvs() ([]domain.EnvVar, error) {
	if err := s.validateStore(); err != nil {
		return nil, err
	}

	out := make([]domain.EnvVar, 0)
	s.deps.Store.ReadSettings(func(st ports.SettingsAggregate) {
		for key, value := range st.Envs {
			out = append(out, domain.EnvVar{Key: key, Value: value})
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (s *Service) ReplaceEnvs(in map[string]string) ([]domain.EnvVar, error) {
	if err := s.validateStore(); err != nil {
		return nil, err
	}

	normalized := map[string]string{}
	for key, value := range in {
		name := strings.TrimSpace(key)
		if name == "" {
			return nil, &ValidationError{
				Code:    "invalid_env_key",
				Message: "env key cannot be empty",
			}
		}
		normalized[name] = value
	}

	if err := s.deps.Store.WriteSettings(func(st *ports.SettingsAggregate) error {
		st.Envs = normalized
		return nil
	}); err != nil {
		return nil, err
	}
	return s.ListEnvs()
}

func (s *Service) DeleteEnv(key string) ([]domain.EnvVar, bool, error) {
	if err := s.validateStore(); err != nil {
		return nil, false, err
	}

	exists := false
	if err := s.deps.Store.WriteSettings(func(st *ports.SettingsAggregate) error {
		if _, ok := st.Envs[key]; ok {
			exists = true
			delete(st.Envs, key)
		}
		return nil
	}); err != nil {
		return nil, false, err
	}
	if !exists {
		return nil, false, nil
	}

	out, err := s.ListEnvs()
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func (s *Service) ListSkills(onlyEnabled bool) ([]domain.SkillSpec, error) {
	if err := s.validateStore(); err != nil {
		return nil, err
	}

	out := make([]domain.SkillSpec, 0)
	s.deps.Store.ReadSettings(func(st ports.SettingsAggregate) {
		for _, spec := range st.Skills {
			if onlyEnabled && !spec.Enabled {
				continue
			}
			out = append(out, cloneSkillSpec(spec))
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Service) BatchSetSkillEnabled(names []string, enabled bool) error {
	if err := s.validateStore(); err != nil {
		return err
	}

	return s.deps.Store.WriteSettings(func(st *ports.SettingsAggregate) error {
		for _, name := range names {
			item, ok := st.Skills[name]
			if !ok {
				continue
			}
			item.Enabled = enabled
			st.Skills[name] = item
		}
		return nil
	})
}

func (s *Service) CreateSkill(input CreateSkillInput) (bool, error) {
	if err := s.validateStore(); err != nil {
		return false, err
	}

	name := strings.TrimSpace(input.Name)
	content := strings.TrimSpace(input.Content)
	if name == "" || content == "" {
		return false, &ValidationError{
			Code:    "invalid_skill",
			Message: "name and content are required",
		}
	}

	if err := s.deps.Store.WriteSettings(func(st *ports.SettingsAggregate) error {
		st.Skills[name] = domain.SkillSpec{
			Name:       name,
			Content:    input.Content,
			Source:     "customized",
			Path:       filepath.Join(s.deps.DataDir, "skills", name),
			References: safeMap(input.References),
			Scripts:    safeMap(input.Scripts),
			Enabled:    true,
		}
		return nil
	}); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) SetSkillEnabled(name string, enabled bool) (bool, error) {
	if err := s.validateStore(); err != nil {
		return false, err
	}

	exists := false
	if err := s.deps.Store.WriteSettings(func(st *ports.SettingsAggregate) error {
		item, ok := st.Skills[name]
		if !ok {
			return nil
		}
		exists = true
		item.Enabled = enabled
		st.Skills[name] = item
		return nil
	}); err != nil {
		return false, err
	}
	return exists, nil
}

func (s *Service) DeleteSkill(name string) (bool, error) {
	if err := s.validateStore(); err != nil {
		return false, err
	}

	deleted := false
	if err := s.deps.Store.WriteSettings(func(st *ports.SettingsAggregate) error {
		if _, ok := st.Skills[name]; ok {
			delete(st.Skills, name)
			deleted = true
		}
		return nil
	}); err != nil {
		return false, err
	}
	return deleted, nil
}

func (s *Service) LoadSkillFile(name string, filePath string) (string, bool, error) {
	if err := s.validateStore(); err != nil {
		return "", false, err
	}

	content := ""
	found := false
	s.deps.Store.ReadSettings(func(st ports.SettingsAggregate) {
		skill, ok := st.Skills[name]
		if !ok {
			return
		}
		content, found = ReadSkillVirtualFile(skill, filePath)
	})
	return content, found, nil
}

func (s *Service) ListChannels() (domain.ChannelConfigMap, error) {
	if err := s.validateStore(); err != nil {
		return nil, err
	}

	out := domain.ChannelConfigMap{}
	s.deps.Store.ReadSettings(func(st ports.SettingsAggregate) {
		out = cloneChannelConfigMap(st.Channels)
	})
	return out, nil
}

func (s *Service) ListChannelTypes() []string {
	out := make([]string, 0, len(s.deps.SupportedChannels))
	for name := range s.deps.SupportedChannels {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (s *Service) ReplaceChannels(in domain.ChannelConfigMap) (domain.ChannelConfigMap, error) {
	if err := s.validateStore(); err != nil {
		return nil, err
	}

	normalized := domain.ChannelConfigMap{}
	for name, cfg := range in {
		key := strings.ToLower(strings.TrimSpace(name))
		if !s.channelSupported(key) {
			return nil, &ValidationError{
				Code:    "channel_not_supported",
				Message: fmt.Sprintf("channel %q is not supported", name),
			}
		}
		normalized[key] = cloneChannelConfig(cfg)
	}

	if err := s.deps.Store.WriteSettings(func(st *ports.SettingsAggregate) error {
		st.Channels = normalized
		return nil
	}); err != nil {
		return nil, err
	}
	return cloneChannelConfigMap(in), nil
}

func (s *Service) GetChannel(name string) (map[string]interface{}, bool, error) {
	if err := s.validateStore(); err != nil {
		return nil, false, err
	}

	normalized := strings.ToLower(strings.TrimSpace(name))
	found := false
	var out map[string]interface{}
	s.deps.Store.ReadSettings(func(st ports.SettingsAggregate) {
		out, found = st.Channels[normalized]
		if found {
			out = cloneChannelConfig(out)
		}
	})
	return out, found, nil
}

func (s *Service) PutChannel(name string, body map[string]interface{}) error {
	if err := s.validateStore(); err != nil {
		return err
	}

	normalized := strings.ToLower(strings.TrimSpace(name))
	if !s.channelSupported(normalized) {
		return &ValidationError{
			Code:    "channel_not_supported",
			Message: fmt.Sprintf("channel %q is not supported", name),
		}
	}

	return s.deps.Store.WriteSettings(func(st *ports.SettingsAggregate) error {
		if st.Channels == nil {
			st.Channels = domain.ChannelConfigMap{}
		}
		st.Channels[normalized] = cloneChannelConfig(body)
		return nil
	})
}

func ReadSkillVirtualFile(skill domain.SkillSpec, filePath string) (string, bool) {
	parts := strings.Split(strings.Trim(filePath, "/"), "/")
	if len(parts) < 2 {
		return "", false
	}
	var node interface{}
	switch parts[0] {
	case "references":
		node = skill.References
	case "scripts":
		node = skill.Scripts
	default:
		return "", false
	}
	for _, part := range parts[1:] {
		item, ok := node.(map[string]interface{})
		if !ok {
			return "", false
		}
		next, ok := item[part]
		if !ok {
			return "", false
		}
		node = next
	}
	content, ok := node.(string)
	return content, ok
}

func (s *Service) channelSupported(name string) bool {
	if name == "" {
		return false
	}
	_, ok := s.deps.SupportedChannels[name]
	return ok
}

func (s *Service) validateStore() error {
	if s == nil || s.deps.Store == nil {
		return errors.New("state store is unavailable")
	}
	return nil
}

func safeMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return map[string]interface{}{}
	}
	return in
}

func cloneSkillSpec(spec domain.SkillSpec) domain.SkillSpec {
	return domain.SkillSpec{
		Name:       spec.Name,
		Content:    spec.Content,
		Source:     spec.Source,
		Path:       spec.Path,
		References: cloneJSONMap(spec.References),
		Scripts:    cloneJSONMap(spec.Scripts),
		Enabled:    spec.Enabled,
	}
}

func cloneChannelConfigMap(in domain.ChannelConfigMap) domain.ChannelConfigMap {
	out := domain.ChannelConfigMap{}
	for name, cfg := range in {
		out[name] = cloneChannelConfig(cfg)
	}
	return out
}

func cloneChannelConfig(in map[string]interface{}) map[string]interface{} {
	return cloneJSONMap(in)
}

func cloneJSONMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return map[string]interface{}{}
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return cloneJSONMapShallow(in)
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return cloneJSONMapShallow(in)
	}
	return out
}

func cloneJSONMapShallow(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
