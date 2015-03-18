package main

import (
	"flag"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"gopkg.in/errgo.v1"
	"gopkg.in/hockeypuck/hkp.v0/sks"
	log "gopkg.in/hockeypuck/logrus.v0"
	"gopkg.in/hockeypuck/openpgp.v0"

	"github.com/hockeypuck/server"
	"github.com/hockeypuck/server/cmd"
)

var (
	configFile = flag.String("config", "", "config file")
	cpuProf    = flag.Bool("cpuprof", false, "enable CPU profiling")
	memProf    = flag.Bool("memprof", false, "enable mem profiling")
)

func main() {
	flag.Parse()

	var (
		settings *server.Settings
		err      error
	)
	if configFile != nil {
		conf, err := ioutil.ReadFile(*configFile)
		if err != nil {
			cmd.Die(errgo.Mask(err))
		}
		settings, err = server.ParseSettings(string(conf))
		if err != nil {
			cmd.Die(errgo.Mask(err))
		}
	}

	cpuFile := cmd.StartCPUProf(*cpuProf, nil)

	args := flag.Args()
	if len(args) == 0 {
		log.Error("usage: %s [flags] <file1> [file2 .. fileN]", os.Args[0])
		cmd.Die(errgo.New("missing PGP key file arguments"))
	}

	st, err := server.DialStorage(settings)
	if err != nil {
		cmd.Die(errgo.Mask(err))
	}
	sksPeer, err := sks.NewPeer(st, settings.Conflux.Recon.LevelDB.Path, &settings.Conflux.Recon.Settings)
	if err != nil {
		cmd.Die(errgo.Mask(err))
	}

	c := make(chan os.Signal)
	signal.Notify(c, syscall.SIGUSR2)
	go func() {
		for {
			select {
			case sig := <-c:
				switch sig {
				case syscall.SIGUSR2:
					cpuFile = cmd.StartCPUProf(*cpuProf, cpuFile)
					cmd.WriteMemProf(*memProf)
				}
			}
		}
	}()

	for _, arg := range args {
		matches, err := filepath.Glob(arg)
		if err != nil {
			log.Errorf("failed to match %q: %v", arg, err)
			continue
		}
		for _, file := range matches {
			f, err := os.Open(file)
			if err != nil {
				log.Errorf("failed to open %q for reading: %v", file, err)
			}
			var keys []*openpgp.PrimaryKey
			for kr := range openpgp.ReadKeys(f) {
				if kr.Error != nil {
					log.Errorf("error reading key: %v", errgo.Details(kr.Error))
				} else {
					keys = append(keys, kr.PrimaryKey)
				}
			}
			t := time.Now()
			err = st.Insert(keys)
			if err != nil {
				log.Errorf("failed to insert keys from %q: %v", file, errgo.Details(err))
			} else {
				log.Infof("loaded %d keys from %q in %v", len(keys), file, time.Since(t))
			}
		}
	}
	sksPeer.WriteStats()

	cmd.Die(err)
}
