package main

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

func main() {
	oldYAML := `cache_path: "~/.streamd.cache"
chat_messages_storage: "~/.streamd.chat"
git_repo:
  enable: true
  url: "git@github.com:user/repo.git"
backends:
  twitch:
    enable: true
    config:
      channel: "channel1"
      client_id: "clientid1"
    stream_profiles:
      profile1:
        parent: "parent1"
        order: 10
        tags: ["tag1", "tag2"]
      profile2:
        order: 20
    custom:
      key1: "value1"
      key2: 42
  obs:
    enable: true
    config:
      host: "localhost"
    stream_profiles:
      obs_p1:
        order: 5
      obs_p2: {}
profile_metadata:
  profile1:
    default_stream_title: "Title 1"
  profile2:
    default_stream_title: "Title 2"
monitor:
  elements:
    elem1:
      width: 100
    elem2:
      width: 200
`
	var raw map[string]any
	err := yaml.Unmarshal([]byte(oldYAML), &raw)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	out, _ := yaml.Marshal(raw)
	err = os.WriteFile("old_config.yaml", out, 0644)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Generated old_config.yaml")
}
