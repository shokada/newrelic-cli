package configuration

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type ConfigKey string

type ConfigProvider struct {
	cfg             []byte
	fields          []FieldDefinition
	fileName        string
	scope           string
	mu              sync.Mutex
	dirty           bool
	explicitValues  bool
	settingDefaults bool
}

type FieldDefinition struct {
	EnvVar         string
	Key            ConfigKey
	Default        interface{}
	CaseSensitive  bool
	Sensitive      bool
	ValidationFunc ConfigValueValidationFunc
}

type ConfigValueValidationFunc func(key ConfigKey, value interface{}) error

func IntGreaterThan(greaterThan int) func(key ConfigKey, value interface{}) error {
	return func(key ConfigKey, value interface{}) error {
		var s int
		var ok bool
		if s, ok = value.(int); !ok {
			return fmt.Errorf("%v is not an int", value)
		}

		if s > greaterThan {
			return nil
		}

		return fmt.Errorf("value %d is not greater than %d", s, greaterThan)
	}
}

func StringInStrings(caseSensitive bool, allowedValues ...string) func(key ConfigKey, value interface{}) error {
	return func(key ConfigKey, value interface{}) error {
		var s string
		var ok bool
		if s, ok = value.(string); !ok {
			return fmt.Errorf("%v is not a string", value)
		}

		for _, v := range allowedValues {
			if caseSensitive && s == v {
				return nil
			}

			if !caseSensitive && strings.EqualFold(s, v) {
				return nil
			}
		}

		return fmt.Errorf("value %v not in allowed values: %s", s, allowedValues)
	}
}

type ConfigProviderOption func(*ConfigProvider) error

func NewConfigProvider(opts ...ConfigProviderOption) (*ConfigProvider, error) {
	p := &ConfigProvider{}

	for _, fn := range opts {
		if fn == nil {
			continue
		}
		if err := fn(p); err != nil {
			return nil, err
		}
	}

	return p, nil
}

func WithExplicitValues() ConfigProviderOption {
	return func(p *ConfigProvider) error {
		p.explicitValues = true
		return nil
	}
}

func WithFieldDefinitions(definitions ...FieldDefinition) ConfigProviderOption {
	return func(p *ConfigProvider) error {
		for _, d := range definitions {
			if !d.CaseSensitive {
				for _, k := range p.getConfigValueKeys() {
					if strings.EqualFold(string(k), string(d.Key)) {
						return fmt.Errorf("unable to add case-insensitive field definition for %s, another field already defined with matching case-folded key", d.Key)
					}
				}
			}
			p.fields = append(p.fields, d)
		}
		return nil
	}
}

func WithFilePersistence(fileName string) ConfigProviderOption {
	return func(p *ConfigProvider) error {
		p.fileName = fileName
		return nil
	}
}

func WithScope(scope string) ConfigProviderOption {
	return func(p *ConfigProvider) error {
		p.scope = scope
		return nil
	}
}

func (p *ConfigProvider) GetInt(key ConfigKey) (int64, error) {
	return p.GetIntWithScope("", key)
}

func (p *ConfigProvider) GetString(key ConfigKey) (string, error) {
	return p.GetStringWithScope("", key)
}

func (p *ConfigProvider) GetIntWithScope(scope string, key ConfigKey) (int64, error) {
	v, err := p.GetWithScope(scope, key)
	if err != nil {
		return 0, err
	}

	switch v := v.(type) {
	case float64:
		return int64(v), nil
	case string:
		i, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("value %v for key %s is not an int", v, key)
		}
		return int64(i), nil
	}

	return 0, fmt.Errorf("value %v for key %s is not an int", v, key)
}

func (p *ConfigProvider) GetStringWithScope(scope string, key ConfigKey) (string, error) {
	v, err := p.GetWithScope(scope, key)
	if err != nil {
		return "", err
	}

	if s, ok := v.(string); ok {
		return s, nil
	}

	return "", fmt.Errorf("value %v for key %s is not a string", v, key)
}

func (p *ConfigProvider) Get(key ConfigKey) (interface{}, error) {
	return p.GetWithScope("", key)
}

func (p *ConfigProvider) GetWithScope(scope string, key ConfigKey) (interface{}, error) {
	d := p.getFieldDefinition(key)

	if d != nil {
		if e, ok := os.LookupEnv(d.EnvVar); ok {
			return e, nil
		}

		if !d.CaseSensitive {
			// use the case convention from the field definition
			key = d.Key
		}
	}

	path := string(key)
	if scope != "" {
		path = fmt.Sprintf("%s.%s", scope, key)
	}

	if p.scope != "" {
		path = fmt.Sprintf("%s.%s", p.scope, key)
	}

	res, err := p.getFromConfig(path)
	if err != nil {
		return "", err
	}

	return res.Value(), nil
}

func (p *ConfigProvider) Set(key ConfigKey, value interface{}) error {
	return p.SetWithScope("", key, value)
}

func (p *ConfigProvider) SetWithScope(scope string, key ConfigKey, value interface{}) error {
	v := p.getFieldDefinition(key)

	if v != nil {
		if v.ValidationFunc != nil {
			if err := v.ValidationFunc(key, value); err != nil {
				return err
			}
		}

		if !v.CaseSensitive {
			// use the case convention from the field definition
			key = v.Key
		}
	} else if p.explicitValues {
		return fmt.Errorf("key '%s' is not valid, valid keys are: %v", key, p.getConfigValueKeys())
	}

	path := string(key)
	if scope != "" {
		path = fmt.Sprintf("%s.%s", scope, key)
	}

	if p.scope != "" {
		path = fmt.Sprintf("%s.%s", p.scope, key)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	cfg, err := sjson.Set(p.getConfig(), escapeWildcards(path), value)
	if err != nil {
		return err
	}

	if err := p.writeConfig(cfg); err != nil {
		return err
	}

	return nil
}

func (p *ConfigProvider) RemoveScope(scope string) error {
	path := scope
	if p.scope != "" {
		path = fmt.Sprintf("%s.%s", p.scope, scope)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	cfg, err := sjson.Delete(p.getConfig(), path)
	if err != nil {
		return err
	}

	if err := p.writeConfig(cfg); err != nil {
		return err
	}

	return nil
}

func (p *ConfigProvider) VisitAllFieldsWithScope(scope string, fn func(d FieldDefinition)) {
	for _, f := range p.fields {
		fn(f)
	}
}

func (p *ConfigProvider) GetScopes() []string {
	s := []string{}
	result := gjson.Get(p.getConfig(), "@this")
	result.ForEach(func(key, value gjson.Result) bool {
		s = append(s, key.String())
		return true
	})

	return s
}

func (p *ConfigProvider) getFromConfig(path string) (*gjson.Result, error) {
	res := gjson.Get(p.getConfig(), path)

	if !res.Exists() {
		return nil, fmt.Errorf("no value found at path %s", path)
	}

	return &res, nil
}

func (p *ConfigProvider) writeConfig(json string) error {
	p.dirty = true
	p.cfg = []byte(json)

	if p.fileName != "" {
		if err := os.WriteFile(p.fileName, p.cfg, 0644); err != nil {
			return err
		}
	}

	return nil
}

func (p *ConfigProvider) getConfig() string {
	if !p.settingDefaults && (p.cfg == nil || p.dirty) {
		if p.fileName != "" {
			p.setConfigFromFile()
		}

		if p.cfg == nil || len(p.cfg) == 0 {
			// Flip settingDefaults true to avoid infinite recursion over getConfig()
			p.settingDefaults = true
			p.setDefaultConfig()
			p.settingDefaults = false
		}

		p.dirty = false
	}

	return string(p.cfg)
}

func (p *ConfigProvider) setConfigFromFile() {
	data, err := os.ReadFile(p.fileName)
	if err != nil {
		return
	}

	p.cfg = data
}

func (p *ConfigProvider) setDefaultConfig() {
	for _, v := range p.fields {
		if v.Default != nil {
			err := p.Set(v.Key, v.Default)
			if err != nil {
				log.Fatalf("could not write default config settings")
			}
		}
	}

}

// Escape wildcard characters, as required by sjson
func escapeWildcards(key string) string {
	re := regexp.MustCompile(`([*?])`)
	return re.ReplaceAllString(key, "\\$1")
}

func (p *ConfigProvider) getFieldDefinition(key ConfigKey) *FieldDefinition {
	for _, v := range p.fields {
		if !v.CaseSensitive && strings.EqualFold(string(key), string(v.Key)) {
			return &v
		}

		if v.CaseSensitive && key == v.Key {
			return &v
		}
	}

	return nil
}

func (p *ConfigProvider) getConfigValueKeys() []ConfigKey {
	var keys []ConfigKey
	for _, v := range p.fields {
		keys = append(keys, v.Key)
	}

	return keys
}
