package nsca

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/tubemogul/nscatools"
)

func Handler(p *nscatools.DataPacket) error {
	log.Debug().
		Int16("version", p.Version).
		Uint32("crc", p.Crc).
		Uint32("timestamp", p.Timestamp).
		Int16("state", p.State).
		Str("hostname", p.HostName).
		Str("service", p.Service).
		Str("pluginOutput", p.PluginOutput).
		Msg("processing DataPacket")

	return nil
}

func Start(ctx context.Context) {
	nscaHost := "localhost"
	nscaPort := uint16(5667)
	nscaEncrypt := nscatools.EncryptNone
	nscaPassword := ""
	nscaEnvHost := os.Getenv("NSCA_HOST")
	nscaEnvPort := os.Getenv("NSCA_PORT")
	nscaEnvEncrypt := os.Getenv("NSCA_ENCRYPT")
	nscaEnvPassword := os.Getenv("NSCA_PASSWORD")
	if len(nscaEnvHost) > 0 {
		nscaHost = nscaEnvHost
	}
	if len(nscaEnvPort) > 0 {
		if i, err := strconv.Atoi(nscaEnvPort); err == nil {
			nscaPort = uint16(i)
		}
	}
	if len(nscaEnvEncrypt) > 0 {
		if i, err := strconv.Atoi(nscaEnvEncrypt); err == nil {
			nscaEncrypt = int(i)
		}
	}
	if len(nscaEnvPassword) > 0 {
		nscaPassword = nscaEnvPassword
	}

	// stdlog.SetOutput(log.Logger)
	// nscatools.StartServer(
	// 	nscatools.NewConfig(nscaHost, nscaPort, nscaEncrypt, nscaPassword, Handler),
	// 	true)
	StartServerWithContext(ctx,
		nscatools.NewConfig(nscaHost, nscaPort, nscaEncrypt, nscaPassword, Handler))
}

func StartServerWithContext(ctx context.Context, conf *nscatools.Config) error {
	service := fmt.Sprint(conf.Host, ":", conf.Port)
	tcpAddr, err := net.ResolveTCPAddr("tcp", service)
	if err != nil {
		return fmt.Errorf("unable to resolve address: %w", err)
	}

	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		return fmt.Errorf("unable to open a TCP listener: %w", err)
	}
	defer listener.Close()

	log.Info().Msgf("start tcp listener %s", service)
	for {
		select {
		case <-ctx.Done():
			log.Info().Msgf("stop tcp listener %s", service)
			return nil

		default:
			if err := listener.SetDeadline(time.Now().Add(time.Second)); err != nil {
				return err
			}
			conn, err := listener.Accept()
			if err != nil {
				if os.IsTimeout(err) {
					continue
				}
				log.Err(err).Msg("could not accept")
			}
			defer conn.Close()

			// run as a goroutine
			go HandleClient(conf, conn)
		}
	}
}

func HandleClient(conf *nscatools.Config, conn net.Conn) error {
	// close connection on exit
	defer conn.Close()

	// sends the initialization packet
	ipacket, err := nscatools.NewInitPacket()
	if err != nil {
		log.Err(err).Msg("unable to create the init packet")
		return err
	}
	if err = ipacket.Write(conn); err != nil {
		log.Err(err).Msg("unable to send the init packet")
		return err
	}

	// Retrieves the data from the client
	data := nscatools.NewDataPacket(conf.EncryptionMethod, []byte(conf.Password), ipacket)

	if err = data.Read(conn); err != nil {
		log.Err(err).Msg("unable to read the data packet")
		return err
	}
	if err = conf.PacketHandler(data); err != nil {
		log.Err(err).Msg("unable to process the data packet in the custom handler")
	}
	return err
}
