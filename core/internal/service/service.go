package service

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func Definition(executable string) (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	switch runtime.GOOS {
	case "darwin":
		path := filepath.Join(home, "Library", "LaunchAgents", "com.teamwork.hub.plist")
		body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>com.teamwork.hub</string><key>ProgramArguments</key><array><string>%s</string><string>serve</string></array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/></dict></plist>`, executable)
		return path, body, nil
	case "linux":
		config := os.Getenv("XDG_CONFIG_HOME")
		if config == "" {
			config = filepath.Join(home, ".config")
		}
		path := filepath.Join(config, "systemd", "user", "teamwork.service")
		body := fmt.Sprintf("[Unit]\nDescription=TeamWork Integration Hub\nAfter=network-online.target\n\n[Service]\nExecStart=%s serve\nRestart=on-failure\n\n[Install]\nWantedBy=default.target\n", executable)
		return path, body, nil
	default:
		return "", "", fmt.Errorf("service manager unsupported on %s", runtime.GOOS)
	}
}

func Install(executable string) (string, error) {
	path, body, err := Definition(executable)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		return "", err
	}
	return path, nil
}
