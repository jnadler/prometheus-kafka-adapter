// Copyright 2018 Telefónica
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"time"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/gin-gonic/contrib/ginrus"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"github.com/sirupsen/logrus"
)

func main() {
	log.Info("creating kafka producer")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	kafkaConfig := kafka.ConfigMap{
		"bootstrap.servers":   kafkaBrokerList,
		"compression.codec":   kafkaCompression,
		"batch.num.messages":  kafkaBatchNumMessages,
		"go.batch.producer":   true,  // Enable batch producer (for increased performance).
		"go.delivery.reports": true, // per-message delivery reports to the Events() channel
	}

	if kafkaSslClientCertFile != "" && kafkaSslClientKeyFile != "" && kafkaSslCACertFile != "" {
		if kafkaSecurityProtocol == "" {
			kafkaSecurityProtocol = "ssl"
		}

		if kafkaSecurityProtocol != "ssl" && kafkaSecurityProtocol != "sasl_ssl" {
			logrus.Fatal("invalid config: kafka security protocol is not ssl based but ssl config is provided")
		}

		kafkaConfig["security.protocol"] = kafkaSecurityProtocol
		kafkaConfig["ssl.ca.location"] = kafkaSslCACertFile              // CA certificate file for verifying the broker's certificate.
		kafkaConfig["ssl.certificate.location"] = kafkaSslClientCertFile // Client's certificate
		kafkaConfig["ssl.key.location"] = kafkaSslClientKeyFile          // Client's key
		kafkaConfig["ssl.key.password"] = kafkaSslClientKeyPass          // Key password, if any.
	}

	if kafkaSaslMechanism != "" && kafkaSaslUsername != "" && kafkaSaslPassword != "" {
		if kafkaSecurityProtocol != "sasl_ssl" && kafkaSecurityProtocol != "sasl_plaintext" {
			logrus.Fatal("invalid config: kafka security protocol is not sasl based but sasl config is provided")
		}

		kafkaConfig["security.protocol"] = kafkaSecurityProtocol
		kafkaConfig["sasl.mechanism"] = kafkaSaslMechanism
		kafkaConfig["sasl.username"] = kafkaSaslUsername
		kafkaConfig["sasl.password"] = kafkaSaslPassword
	}

	producer, err := kafka.NewProducer(&kafkaConfig)

	// read delivery reports and log errors
	go func() {
		for event := range producer.Events() {
			// confluent-kafka-go docs say that this should be '*kafka.Message', but apparently it is 'kafka.Error'
			produceError, ok := event.(kafka.Error)
			if ok {
				logrus.WithField("error_code", produceError.Code).Errorf("failed to produce message: %s", produceError.String())
				produceError.String()
			}

			producedMessage, ok := event.(*kafka.Message)
			if ok {
				if producedMessage.TopicPartition.Error != nil {
					logrus.WithError(producedMessage.TopicPartition.Error).WithField("topic", producedMessage.TopicPartition.Topic).Error("failed to produce message to topic")
				}
			}
		}
	}()

	if err != nil {
		logrus.WithError(err).Fatal("couldn't create kafka producer")
	}

	if kafkaPartitionLabels != nil {
		if err := syncTopicMetadata(ctx, producer); err != nil {
			logrus.WithError(err).Fatal("couldn't fetch topic metadata")
		}
	}

	r := gin.New()

	r.Use(ginrus.Ginrus(logrus.StandardLogger(), time.RFC3339, true), gin.Recovery())

	r.GET("/metrics", gin.WrapH(prometheus.UninstrumentedHandler()))
	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"status": "UP"}) })
	if basicauth {
		authorized := r.Group("/", gin.BasicAuth(gin.Accounts{
			basicauthUsername: basicauthPassword,
		}))
		authorized.POST("/receive", receiveHandler(producer, serializer))
	} else {
		r.POST("/receive", receiveHandler(producer, serializer))
	}

	logrus.Fatal(r.Run())
}
