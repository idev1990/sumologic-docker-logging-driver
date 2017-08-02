package main

import (
  "compress/gzip"
  "context"
  "crypto/tls"
  "io/ioutil"
  "net/url"
  "os"
  "strconv"
  "testing"
  "time"

  "github.com/docker/docker/daemon/logger"
  "github.com/sirupsen/logrus"
  "github.com/stretchr/testify/assert"
  "github.com/tonistiigi/fifo"
  "golang.org/x/sys/unix"
)

const (
  filePath  = "/tmp/test"
  filePath1 = "/tmp/test1"
  filePath2 = "/tmp/test2"

  testHttpSourceUrl = "https://example.org"
  testProxyUrlStr = "https://example.org"

  testSource = "sumo-test"
  testTime = 1234567890
  testIsPartial = false
)

var (
  testLine = []byte("a test log message")
)

func TestDriversDefaultConfig (t *testing.T) {
  testLoggersCount := 100

  for i := 0; i < testLoggersCount; i++ {
    testFifo, err := fifo.OpenFifo(context.Background(), filePath + strconv.Itoa(i + 1), unix.O_RDWR|unix.O_CREAT|unix.O_NONBLOCK, fileMode)
    assert.Nil(t, err)
    defer testFifo.Close()
    defer os.Remove(filePath + strconv.Itoa(i + 1))
  }

  info := logger.Info{
    Config: map[string]string{
      logOptUrl: testHttpSourceUrl,
    },
    ContainerID: "containeriid",
  }

  t.Run("startLoggingInternal", func(t *testing.T) {
    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger1, err := testSumoDriver.startLoggingInternal(filePath1, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger1.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, false, testSumoLogger1.gzipCompression, "compression not specified, should be false")
    assert.Equal(t, gzip.DefaultCompression, testSumoLogger1.gzipCompressionLevel, "compression level not specified, should be default value")
    assert.Equal(t, defaultSendingFrequency, testSumoLogger1.sendingFrequency, "sending frequency not specified, should be default value")
    assert.Equal(t, defaultQueueSize, cap(testSumoLogger1.logQueue), "queue size not specified, should be default value")
    assert.Equal(t, defaultBatchSize, testSumoLogger1.batchSize, "batch size not specified, should be default value")
    assert.Equal(t, &tls.Config{}, testSumoLogger1.tlsConfig, "no tls configs specified, should be default value")
    assert.Nil(t, testSumoLogger1.proxyUrl, "no proxy url specified, should be default value")

    _, err = testSumoDriver.startLoggingInternal(filePath1, info)
    assert.Error(t, err, "trying to call StartLogging for filepath that already exists should return error")
    assert.Equal(t, 1, len(testSumoDriver.loggers),
      "there should still be one logger after calling StartLogging for filepath that already exists")

    testSumoLogger2, err := testSumoDriver.startLoggingInternal(filePath2, info)
    assert.Nil(t, err)
    assert.Equal(t, 2, len(testSumoDriver.loggers),
      "there should be two loggers now after calling StartLogging on driver for different filepaths")
    assert.Equal(t, info.Config[logOptUrl], testSumoLogger2.httpSourceUrl, "http source url should be configured correctly")

    err = testSumoDriver.StopLogging(filePath1)
    assert.Nil(t, err, "trying to call StopLogging for existing logger should not return error")
    assert.Equal(t, 1, len(testSumoDriver.loggers), "calling StopLogging on existing logger should remove the logger")
    err = testSumoDriver.StopLogging(filePath2)
    assert.Nil(t, err, "trying to call StopLogging for existing logger should not return error")
    assert.Equal(t, 0, len(testSumoDriver.loggers), "calling StopLogging on existing logger should remove the logger")
  })

  t.Run("StopLogging", func(t *testing.T) {
    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    err := testSumoDriver.StopLogging(filePath1)
    assert.Nil(t, err, "trying to call StopLogging for nonexistent logger should NOT return error")
    assert.Equal(t, 0, len(testSumoDriver.loggers), "no loggers should be changed after calling StopLogging for nonexistent logger")

    _, err = testSumoDriver.startLoggingInternal(filePath1, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")

    err = testSumoDriver.StopLogging(filePath2)
    assert.Nil(t, err, "trying to call StopLogging for nonexistent logger should NOT return error")
    assert.Equal(t, 1, len(testSumoDriver.loggers), "no loggers should be changed after calling StopLogging for nonexistent logger")

    err = testSumoDriver.StopLogging(filePath1)
    assert.Nil(t, err, "trying to call StopLogging for existing logger should not return error")
    assert.Equal(t, 0, len(testSumoDriver.loggers), "calling StopLogging on existing logger should remove the logger")
  })

  t.Run("startLoggingInternal, concurrently", func(t *testing.T) {
    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    waitForAllLoggers := make(chan int)
    for i := 0; i < testLoggersCount; i++ {
      go func(i int) {
        _, err := testSumoDriver.startLoggingInternal(filePath + strconv.Itoa(i + 1), info)
        assert.Nil(t, err)
        waitForAllLoggers <- i
      }(i)
    }
    for i := 0; i < testLoggersCount; i ++ {
      <-waitForAllLoggers
    }
    assert.Equal(t, testLoggersCount, len(testSumoDriver.loggers),
      "there should be %v loggers now after calling StartLogging on driver that many times on different filepaths", testLoggersCount)
  })
}

func TestDriversLogOpts (t *testing.T) {
  logrus.SetOutput(ioutil.Discard)

  testFifo, err := fifo.OpenFifo(context.Background(), filePath, unix.O_RDWR|unix.O_CREAT|unix.O_NONBLOCK, fileMode)
  assert.Nil(t, err)
  defer testFifo.Close()
  defer os.Remove(filePath)

  testProxyUrl, _ := url.Parse(testProxyUrlStr)
  testInsecureSkipVerify := true
  testServerName := "sumologic.net"

  testTlsConfig := &tls.Config{
    InsecureSkipVerify: testInsecureSkipVerify,
    ServerName: testServerName,
  }

  testGzipCompression := true
  testGzipCompressionLevel := gzip.BestCompression
  testSendingFrequency := time.Second
  testQueueSize := 2000
  testBatchSize := 1000

  t.Run("startLoggingInternal with correct log opts", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: strconv.Itoa(testGzipCompressionLevel),
        logOptSendingFrequency: testSendingFrequency.String(),
        logOptQueueSize: strconv.Itoa(testQueueSize),
        logOptBatchSize: strconv.Itoa(testBatchSize),
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.startLoggingInternal(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, testGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified, should be specified value")
    assert.Equal(t, testSendingFrequency, testSumoLogger.sendingFrequency, "sending frequency specified, should be specified value")
    assert.Equal(t, testQueueSize, cap(testSumoLogger.logQueue), "queue size specified, should be specified value")
    assert.Equal(t, testBatchSize, testSumoLogger.batchSize, "batch size specified, should be specified value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("startLoggingInternal with bad insecure skip verify", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: "truee",
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: strconv.Itoa(testGzipCompressionLevel),
        logOptSendingFrequency: testSendingFrequency.String(),
        logOptQueueSize: strconv.Itoa(testQueueSize),
        logOptBatchSize: strconv.Itoa(testBatchSize),
      },
      ContainerID: "containeriid",
    }

    testTlsConfigNoInsecureSkipVerify := &tls.Config{
      InsecureSkipVerify: defaultInsecureSkipVerify,
      ServerName: testServerName,
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.startLoggingInternal(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, testGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified, should be specified value")
    assert.Equal(t, testSendingFrequency, testSumoLogger.sendingFrequency, "sending frequency specified, should be specified value")
    assert.Equal(t, testQueueSize, cap(testSumoLogger.logQueue), "queue size specified, should be specified value")
    assert.Equal(t, testBatchSize, testSumoLogger.batchSize, "batch size specified, should be specified value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfigNoInsecureSkipVerify, testSumoLogger.tlsConfig,
      "server name specified, should be specified value; insecure skip verify specified incorrectly, should be default value")
  })

  t.Run("startLoggingInternal with bad gzip compression", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: "truee",
        logOptGzipCompressionLevel: strconv.Itoa(testGzipCompressionLevel),
        logOptSendingFrequency: testSendingFrequency.String(),
        logOptQueueSize: strconv.Itoa(testQueueSize),
        logOptBatchSize: strconv.Itoa(testBatchSize),
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.startLoggingInternal(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, defaultGzipCompression, testSumoLogger.gzipCompression, "compression specified incorrectly, should be default value")
    assert.Equal(t, testGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified, should be specified value")
    assert.Equal(t, testSendingFrequency, testSumoLogger.sendingFrequency, "sending frequency specified, should be specified value")
    assert.Equal(t, testQueueSize, cap(testSumoLogger.logQueue), "queue size specified, should be specified value")
    assert.Equal(t, testBatchSize, testSumoLogger.batchSize, "batch size specified, should be specified value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("startLoggingInternal with bad gzip compression level", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: "2o",
        logOptSendingFrequency: testSendingFrequency.String(),
        logOptQueueSize: strconv.Itoa(testQueueSize),
        logOptBatchSize: strconv.Itoa(testBatchSize),
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.startLoggingInternal(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, defaultGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified incorrectly, should be default value")
    assert.Equal(t, testSendingFrequency, testSumoLogger.sendingFrequency, "sending frequency specified, should be specified value")
    assert.Equal(t, testQueueSize, cap(testSumoLogger.logQueue), "queue size specified, should be specified value")
    assert.Equal(t, testBatchSize, testSumoLogger.batchSize, "batch size specified, should be specified value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("startLoggingInternal with unsupported gzip compression level", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: "20",
        logOptSendingFrequency: testSendingFrequency.String(),
        logOptQueueSize: strconv.Itoa(testQueueSize),
        logOptBatchSize: strconv.Itoa(testBatchSize),
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.startLoggingInternal(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, defaultGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified incorrectly, should be default value")
    assert.Equal(t, testSendingFrequency, testSumoLogger.sendingFrequency, "sending frequency specified, should be specified value")
    assert.Equal(t, testQueueSize, cap(testSumoLogger.logQueue), "queue size specified, should be specified value")
    assert.Equal(t, testBatchSize, testSumoLogger.batchSize, "batch size specified, should be specified value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("startLoggingInternal with bad sending frequency", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: strconv.Itoa(testGzipCompressionLevel),
        logOptSendingFrequency: "72h3n0.5s",
        logOptQueueSize: strconv.Itoa(testQueueSize),
        logOptBatchSize: strconv.Itoa(testBatchSize),
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.startLoggingInternal(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, testGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified, should be specified value")
    assert.Equal(t, defaultSendingFrequency, testSumoLogger.sendingFrequency, "sending frequency specified incorrectly, should be default value")
    assert.Equal(t, testQueueSize, cap(testSumoLogger.logQueue), "queue size specified, should be specified value")
    assert.Equal(t, testBatchSize, testSumoLogger.batchSize, "batch size specified, should be specified value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("startLoggingInternal with unsupported sending frequency", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: strconv.Itoa(testGzipCompressionLevel),
        logOptSendingFrequency: "0s",
        logOptQueueSize: strconv.Itoa(testQueueSize),
        logOptBatchSize: strconv.Itoa(testBatchSize),
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.startLoggingInternal(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, testGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified, should be specified value")
    assert.Equal(t, defaultSendingFrequency, testSumoLogger.sendingFrequency, "sending frequency specified incorrectly, should be default value")
    assert.Equal(t, testQueueSize, cap(testSumoLogger.logQueue), "queue size specified, should be specified value")
    assert.Equal(t, testBatchSize, testSumoLogger.batchSize, "batch size specified, should be specified value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("startLoggingInternal with bad queue size", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: strconv.Itoa(testGzipCompressionLevel),
        logOptSendingFrequency: testSendingFrequency.String(),
        logOptQueueSize: "2ooo",
        logOptBatchSize: strconv.Itoa(testBatchSize),
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.startLoggingInternal(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, testGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified, should be specified value")
    assert.Equal(t, testSendingFrequency, testSumoLogger.sendingFrequency, "sending frequency specified, should be specified value")
    assert.Equal(t, defaultQueueSize, cap(testSumoLogger.logQueue), "queue size specified incorrectly, should be default value")
    assert.Equal(t, testBatchSize, testSumoLogger.batchSize, "batch size specified, should be specified value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("startLoggingInternal with unsupported queue size", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: strconv.Itoa(testGzipCompressionLevel),
        logOptSendingFrequency: testSendingFrequency.String(),
        logOptQueueSize: "-2000",
        logOptBatchSize: strconv.Itoa(testBatchSize),
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.startLoggingInternal(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, testGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified, should be specified value")
    assert.Equal(t, testSendingFrequency, testSumoLogger.sendingFrequency, "sending frequency specified, should be specified value")
    assert.Equal(t, defaultQueueSize, cap(testSumoLogger.logQueue), "queue size specified incorrectly, should be default value")
    assert.Equal(t, testBatchSize, testSumoLogger.batchSize, "batch size specified, should be specified value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("startLoggingInternal with bad batch size", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: strconv.Itoa(testGzipCompressionLevel),
        logOptSendingFrequency: testSendingFrequency.String(),
        logOptQueueSize: strconv.Itoa(testQueueSize),
        logOptBatchSize: "2ooo",
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.startLoggingInternal(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, testGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified, should be specified value")
    assert.Equal(t, testSendingFrequency, testSumoLogger.sendingFrequency, "sending frequency specified, should be specified value")
    assert.Equal(t, testQueueSize, cap(testSumoLogger.logQueue), "queue size specified, should be specified value")
    assert.Equal(t, defaultBatchSize, testSumoLogger.batchSize, "batch size specified incorrectly, should be default value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })

  t.Run("startLoggingInternal with unsupported batch size", func(t *testing.T) {
    info := logger.Info{
      Config: map[string]string{
        logOptUrl: testHttpSourceUrl,
        logOptProxyUrl: testProxyUrlStr,
        logOptInsecureSkipVerify: strconv.FormatBool(testInsecureSkipVerify),
        logOptServerName: testServerName,
        logOptGzipCompression: strconv.FormatBool(testGzipCompression),
        logOptGzipCompressionLevel: strconv.Itoa(testGzipCompressionLevel),
        logOptSendingFrequency: testSendingFrequency.String(),
        logOptQueueSize: strconv.Itoa(testQueueSize),
        logOptBatchSize: "-2000",
      },
      ContainerID: "containeriid",
    }

    testSumoDriver := newSumoDriver()
    assert.Equal(t, 0, len(testSumoDriver.loggers), "there should be no loggers when the driver is initialized")

    testSumoLogger, err := testSumoDriver.startLoggingInternal(filePath, info)
    assert.Nil(t, err)
    assert.Equal(t, 1, len(testSumoDriver.loggers), "there should be one logger after calling StartLogging on driver")
    assert.Equal(t, testHttpSourceUrl, testSumoLogger.httpSourceUrl, "http source url should be configured correctly")
    assert.Equal(t, testGzipCompression, testSumoLogger.gzipCompression, "compression specified, should be specified value")
    assert.Equal(t, testGzipCompressionLevel, testSumoLogger.gzipCompressionLevel, "compression level specified, should be specified value")
    assert.Equal(t, testSendingFrequency, testSumoLogger.sendingFrequency, "sending frequency specified, should be specified value")
    assert.Equal(t, testQueueSize, cap(testSumoLogger.logQueue), "queue size specified, should be specified value")
    assert.Equal(t, defaultBatchSize, testSumoLogger.batchSize, "batch size specified incorrectly, should be default value")
    assert.Equal(t, testProxyUrl, testSumoLogger.proxyUrl, "proxy url specified, should be specified value")
    assert.Equal(t, testTlsConfig, testSumoLogger.tlsConfig, "tls config options specified, should be specified value")
  })
}
