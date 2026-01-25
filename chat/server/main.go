package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"server/internal"

	"github.com/caarlos0/env/v11"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

type Config struct {
	Port                   int    `env:"PORT" envDefault:"8080"`
	KafkaHost              string `env:"KAFKA_HOST"`
	MessagesTopic          string `env:"MESSAGES_TOPIC" envDefault:"messages"`
	TopicPartitions        int    `env:"TOPIC_PARTITIONS" envDefault:"-1"`
	TopicReplicationFactor int    `env:"TOPIC_REPLICATION_FACTOR" envDefault:"-1"`
	LogLevel               string `env:"LOG_LEVEL" envDefault:"info"`
}

func createTopic(logger *zap.SugaredLogger, client *kafka.Client, config *Config) {
	logger.Infof("Creating topic: '%s'", config.MessagesTopic)
	resp, err := client.CreateTopics(context.Background(), &kafka.CreateTopicsRequest{
		Topics: []kafka.TopicConfig{{
			Topic:             config.MessagesTopic,
			NumPartitions:     config.TopicPartitions,
			ReplicationFactor: config.TopicReplicationFactor,
		}},
	})
	if err != nil {
		logger.Errorf("CreateTopics failed with error: %v", err)
		os.Exit(1)
	}
	err = resp.Errors[config.MessagesTopic]
	if err != nil {
		if errors.Is(err, kafka.TopicAlreadyExists) {
			logger.Infof("Topic '%s' already exists, skip", config.MessagesTopic)
		} else {
			logger.Errorf("Unable to create topic %s, error: %v", config.MessagesTopic, err)
			os.Exit(1)
		}
	}
}

func closeKafkaReader(logger *zap.SugaredLogger, reader *kafka.Reader) {
	err := reader.Close()
	if err != nil {
		logger.Errorf("Unable to close Kafka Reader, error: %v", err)
	}
}

func closeKafkaWriter(logger *zap.SugaredLogger, writer *kafka.Writer) {
	err := writer.Close()
	if err != nil {
		logger.Errorf("Unable to close Kafka Writer, error: %v", err)
	}
}

func syncLogger(sugar *zap.SugaredLogger) {
	err := sugar.Sync()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Unable to sync zap logger, error: %v", err)
		os.Exit(1)
	}
}

func main() {
	var config Config
	err := env.Parse(&config)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Unable to parse config, error: %v", err)
		os.Exit(1)
	}
	level, err := zap.ParseAtomicLevel(config.LogLevel)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Unable to parse log level, error: %v", err)
		os.Exit(1)
	}
	zapConfig := zap.NewProductionConfig()
	zapConfig.Level = level
	logger, err := zapConfig.Build()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Unable to create logger, error: %v", err)
		os.Exit(1)
	}
	sugar := logger.Sugar()
	defer syncLogger(sugar)
	client := kafka.Client{
		Addr: kafka.TCP(config.KafkaHost),
	}
	createTopic(sugar, &client, &config)
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{config.KafkaHost},
		Topic:   config.MessagesTopic,
	})
	defer closeKafkaReader(sugar, reader)
	writer := &kafka.Writer{
		Addr:  kafka.TCP(config.KafkaHost),
		Topic: config.MessagesTopic,
	}
	defer closeKafkaWriter(sugar, writer)
	server := internal.NewServer(sugar, config.Port, reader, writer)
	err = server.Serve()
	if err != nil {
		sugar.Errorf("Unable to start server, error: %v", err)
		os.Exit(1)
	}
}
