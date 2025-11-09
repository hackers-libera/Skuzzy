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
}

type LLM struct {
	Name  string `yaml:"name"`
	Type  string `yaml:"type"`
	Model string `yaml:"model"`
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
}

func LoadServerConfig(path string) (ServerConfig,error) {

	var config ServerConfig

	file_contents, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Error loading settings from %v: %v", path, err)
		return config, err
	}
	err = yaml.Unmarshal([]byte(file_contents), &config)
	if err != nil {
		log.Printf("Error loading settings from %v: %v", path, err)
		return config, err
	}

	return config, nil
}
