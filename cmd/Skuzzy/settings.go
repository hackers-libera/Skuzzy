package main

import (
	"gopkg.in/yaml.v3"
	"log"
	"os"
)

type ChannelConfig struct {
	Name              string   `yaml:"name"`
	LLM               string   `yaml:"llm,omitempty"`
	SysPromptsEnabled []string `yaml:"sys_prompts_enabled"`
	Backlog           []string
}

type LLM struct {
	Name  string `yaml:"name"`
	Type  string `yaml:"type"`
	Model string `yaml:"model"`
}

type CTF struct {
	Channel string            `yaml:"channel"`
	Flag    string            `yaml:"flag"`
	Level   int               `yaml:"level"`
	Hints   map[string]string `yaml:"hints"`
	Description string        `yaml:"description,omitempty"`
}

type ServerConfig struct {
	Name                  string            `yaml:"name"`
	Host                  string            `yaml:"host"`
	Nick                  string            `yaml:"nick"`
	NickservUsername      string            `yaml:"nickservusername"`
	NickservPassword      string            `yaml:"nickservpassword"`
	SaslUser              string            `yaml:"sasluser"`
	SaslPassword          string            `yaml:"saslpassword"`
	Channels              []ChannelConfig   `yaml:"channels"`
	LLMS                  []LLM             `yaml:"llms"`
	SysPrompts            map[string]string `yaml:"sys_prompts"`
	SysPromptGlobalPrefix string            `yaml:"sys_prompt_global_suffix"`
	DeepseekAPIKey        string            `yaml:"deepseek_api_key"`
	MaxRemindersPerUser   int               `yaml:"max_reminders_per_user"`
	ServerLogFile         string            `yaml:"server_log_file"`
	RelayBots             []string          `yaml:"relay_bots,omitempty"`
	CtfConfigPath         string            `yaml:"ctf_config_path,omitempty"`
}

type CTFConfig struct {
	CTFFlags map[string]CTF `yaml:"ctf_flags"`
}

func LoadServerConfig(path string) (*ServerConfig, error) {

	var config ServerConfig

	file_contents, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[LoadServerConfig] Error loading settings from %v: %v", path, err)
		return &config, err
	}
	err = yaml.Unmarshal([]byte(file_contents), &config)
	if err != nil {
		log.Printf("[LoadServerConfig] Error loading settings from %v: %v", path, err)
		return &config, err
	}
	log.Printf("[LoadServerConfig] Loaded configuration from %s\n", path)
	return &config, nil
}

func LoadCTFConfig(path string) (*CTFConfig, error) {
	var config CTFConfig

	file_contents, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[LoadCTFConfig] Error loading settings from %v: %v", path, err)
		return &config, err
	}
	err = yaml.Unmarshal([]byte(file_contents), &config)
	if err != nil {
		log.Printf("[LoadCTFConfig] Error loading settings from %v: %v", path, err)
		return &config, err
	}
	log.Printf("[LoadCTFConfig] Loaded configuration from %s\n", path)
	return &config, nil
}
