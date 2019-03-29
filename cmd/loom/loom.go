package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/loomnetwork/loomchain/receipts/leveldb"

	kitprometheus "github.com/go-kit/kit/metrics/prometheus"
	loom "github.com/loomnetwork/go-loom"
	glAuth "github.com/loomnetwork/go-loom/auth"
	"github.com/loomnetwork/go-loom/builtin/commands"
	"github.com/loomnetwork/go-loom/cli"
	"github.com/loomnetwork/go-loom/crypto"
	"github.com/loomnetwork/go-loom/plugin/contractpb"
	"github.com/loomnetwork/go-loom/util"
	"github.com/loomnetwork/loomchain"
	"github.com/loomnetwork/loomchain/abci/backend"
	"github.com/loomnetwork/loomchain/auth"
	d2Oracle "github.com/loomnetwork/loomchain/builtin/plugins/dposv2/oracle"
	d2OracleCfg "github.com/loomnetwork/loomchain/builtin/plugins/dposv2/oracle/config"
	plasmaConfig "github.com/loomnetwork/loomchain/builtin/plugins/plasma_cash/config"
	plasmaOracle "github.com/loomnetwork/loomchain/builtin/plugins/plasma_cash/oracle"

	"github.com/loomnetwork/loomchain/cmd/loom/chainconfig"
	"github.com/loomnetwork/loomchain/cmd/loom/common"
	dbcmd "github.com/loomnetwork/loomchain/cmd/loom/db"
	"github.com/loomnetwork/loomchain/cmd/loom/dbg"
	gatewaycmd "github.com/loomnetwork/loomchain/cmd/loom/gateway"
	"github.com/loomnetwork/loomchain/cmd/loom/replay"
	"github.com/loomnetwork/loomchain/cmd/loom/staking"
	"github.com/loomnetwork/loomchain/config"
	"github.com/loomnetwork/loomchain/eth/polls"
	"github.com/loomnetwork/loomchain/events"
	"github.com/loomnetwork/loomchain/evm"
	tgateway "github.com/loomnetwork/loomchain/gateway"
	karma_handler "github.com/loomnetwork/loomchain/karma"
	"github.com/loomnetwork/loomchain/log"
	"github.com/loomnetwork/loomchain/plugin"
	"github.com/loomnetwork/loomchain/receipts"
	"github.com/loomnetwork/loomchain/receipts/handler"
	regcommon "github.com/loomnetwork/loomchain/registry"
	registry "github.com/loomnetwork/loomchain/registry/factory"
	"github.com/loomnetwork/loomchain/rpc"
	"github.com/loomnetwork/loomchain/store"
	"github.com/loomnetwork/loomchain/throttle"
	"github.com/loomnetwork/loomchain/vm"
	"github.com/pkg/errors"
	stdprometheus "github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ed25519"

	cdb "github.com/loomnetwork/loomchain/db"

	"github.com/loomnetwork/loomchain/fnConsensus"
)

var RootCmd = &cobra.Command{
	Use:   "loom",
	Short: "Loom DAppChain",
}

var codeLoaders map[string]ContractCodeLoader

func init() {
	codeLoaders = map[string]ContractCodeLoader{
		"plugin":   &PluginCodeLoader{},
		"truffle":  &TruffleCodeLoader{},
		"solidity": &SolidityCodeLoader{},
		"hex":      &HexCodeLoader{},
	}
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show the Loom chain version",
		RunE: func(cmd *cobra.Command, args []string) error {
			println(loomchain.FullVersion())
			return nil
		},
	}
}

func printEnv(env map[string]string) {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		val := env[key]
		fmt.Printf("%s = %s\n", key, val)
	}
}

func newEnvCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "env",
		Short: "Show loom config settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := parseConfig()
			if err != nil {
				return err
			}

			printEnv(map[string]string{
				"version":       loomchain.FullVersion(),
				"build":         loomchain.Build,
				"build variant": loomchain.BuildVariant,
				"git sha":       loomchain.GitSHA,
				"go-loom":       loomchain.GoLoomGitSHA,
				"go-ethereum":   loomchain.EthGitSHA,
				"go-plugin":     loomchain.HashicorpGitSHA,
				"plugin path":   cfg.PluginsPath(),
				"peers":         cfg.Peers,
			})
			return nil
		},
	}
}

type genKeyFlags struct {
	PublicFile string `json:"publicfile"`
	PrivFile   string `json:"privfile"`
}

func newGenKeyCommand() *cobra.Command {
	var flags genKeyFlags
	keygenCmd := &cobra.Command{
		Use:   "genkey",
		Short: "generate a public and private key pair",
		RunE: func(cmd *cobra.Command, args []string) error {
			pub, priv, err := ed25519.GenerateKey(nil)
			if err != nil {
				return fmt.Errorf("Error generating key pair: %v", err)
			}
			encoder := base64.StdEncoding
			pubKeyB64 := encoder.EncodeToString(pub[:])
			privKeyB64 := encoder.EncodeToString(priv[:])

			if err := ioutil.WriteFile(flags.PublicFile, []byte(pubKeyB64), 0664); err != nil {
				return fmt.Errorf("Unable to write public key: %v", err)
			}
			if err := ioutil.WriteFile(flags.PrivFile, []byte(privKeyB64), 0664); err != nil {
				return fmt.Errorf("Unable to write private key: %v", err)
			}
			addr := loom.LocalAddressFromPublicKey(pub[:])
			fmt.Printf("local address: %s\n", addr.String())
			fmt.Printf("local address base64: %s\n", encoder.EncodeToString(addr))
			return nil
		},
	}
	keygenCmd.Flags().StringVarP(&flags.PublicFile, "public_key", "a", "", "public key file")
	keygenCmd.Flags().StringVarP(&flags.PrivFile, "private_key", "k", "", "private key file")
	return keygenCmd
}

type yubiHsmFlags struct {
	HsmNewKey  bool   `json:newkey`
	HsmLoadKey bool   `json:loadkey`
	HsmConfig  string `json:config`
}

func newYubiHsmCommand() *cobra.Command {
	var flags yubiHsmFlags
	keygenCmd := &cobra.Command{
		Use:   "yubihsm",
		Short: "generate or load YubiHSM ed25519/secp256k1 key",
		RunE: func(cmd *cobra.Command, args []string) error {
			var yubiPrivKey *crypto.YubiHsmPrivateKey
			var err error

			if len(flags.HsmConfig) == 0 {
				return fmt.Errorf("Please specify YubiHSM configuration file")
			}

			if !flags.HsmLoadKey {
				yubiPrivKey, err = crypto.GenYubiHsmPrivKey(flags.HsmConfig)
			} else {
				yubiPrivKey, err = crypto.LoadYubiHsmPrivKey(flags.HsmConfig)
			}
			if err != nil {
				return fmt.Errorf("Error generating or loading YubiHSM key: %v", err)
			}
			defer yubiPrivKey.UnloadYubiHsmPrivKey()

			fmt.Printf("Private Key Type:   %s\n", yubiPrivKey.GetKeyType())
			fmt.Printf("Private Key ID:     %d\n", yubiPrivKey.GetPrivKeyID())
			fmt.Printf("Public Key address: %s\n", yubiPrivKey.GetPubKeyAddr())

			b64addr, err := yubiPrivKey.GetPubKeyAddrB64Encoded()
			if err != nil {
				fmt.Printf("Public Key address base64-encoded: %v\n", err)
			} else {
				fmt.Printf("Public Key address base64-encoded: %s\n", b64addr)
			}

			return nil
		},
	}
	keygenCmd.Flags().BoolVarP(&flags.HsmNewKey, "new-key", "n", false, "generate YubiHSM ed25519/secp256k1 key")
	keygenCmd.Flags().BoolVarP(&flags.HsmLoadKey, "load-key", "l", false, "load YubiHSM ed25519/secp256k1 key")
	keygenCmd.Flags().StringVarP(&flags.HsmConfig, "hsm-config", "c", "", "yubihsm config")
	return keygenCmd
}

func newInitCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize configs and data",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := parseConfig()
			if err != nil {
				return err
			}
			backend := initBackend(cfg, "", nil)
			if force {
				err = backend.Destroy()
				if err != nil {
					return err
				}
				err = destroyApp(cfg)
				if err != nil {
					return err
				}
				destroyReceiptsDB(cfg)
			}
			validator, err := backend.Init()
			if err != nil {
				return err
			}
			err = initApp(validator, cfg)
			if err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "force initialization")
	return cmd
}

func newResetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Reset the app and blockchain state only",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := parseConfig()
			if err != nil {
				return err
			}

			backend := initBackend(cfg, "", nil)
			err = backend.Reset(0)
			if err != nil {
				return err
			}

			err = resetApp(cfg)
			if err != nil {
				return err
			}

			destroyReceiptsDB(cfg)

			return nil
		},
	}
}

// Generate Or Prints node's ID to the standard output.
func newNodeKeyCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "nodekey",
		Short: "Show node key",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := parseConfig()
			if err != nil {
				return err
			}
			backend := initBackend(cfg, "", nil)
			key, err := backend.NodeKey()
			if err != nil {
				fmt.Printf("Error in determining Node Key")
			} else {
				fmt.Printf("%s\n", key)
			}
			return nil
		},
	}
}

func newRunCommand() *cobra.Command {
	var abciServerAddr string
	var appHeight int64

	cfg, err := parseConfig()

	cmd := &cobra.Command{
		Use:   "run [root contract]",
		Short: "Run the blockchain node",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err != nil {
				return err
			}
			log.Setup(cfg.LoomLogLevel, cfg.LogDestination)

			var fnRegistry fnConsensus.FnRegistry
			if cfg.FnConsensus.Enabled {
				fnRegistry = fnConsensus.NewInMemoryFnRegistry()
			}

			backend := initBackend(cfg, abciServerAddr, fnRegistry)
			loader := plugin.NewMultiLoader(
				plugin.NewManager(cfg.PluginsPath()),
				plugin.NewExternalLoader(cfg.PluginsPath()),
				common.NewDefaultContractsLoader(cfg),
			)

			termChan := make(chan os.Signal)
			go func(c <-chan os.Signal, l plugin.Loader) {
				<-c
				l.UnloadContracts()
				os.Exit(0)
			}(termChan, loader)

			signal.Notify(termChan, syscall.SIGHUP,
				syscall.SIGINT,
				syscall.SIGTERM,
				syscall.SIGQUIT)

			chainID, err := backend.ChainID()
			if err != nil {
				return err
			}

			app, err := loadApp(chainID, cfg, loader, backend, appHeight)
			if err != nil {
				return err
			}
			if err := backend.Start(app); err != nil {
				return err
			}

			nodeSigner, err := backend.NodeSigner()
			if err != nil {
				return err
			}

			if err := initQueryService(app, chainID, cfg, loader, app.ReceiptHandlerProvider); err != nil {
				return err
			}

			if err := startGatewayOracle(chainID, cfg.TransferGateway); err != nil {
				return err
			}

			if err := startGatewayFn(chainID, fnRegistry, cfg.TransferGateway, nodeSigner); err != nil {
				return err
			}

			if err := startLoomCoinGatewayOracle(chainID, cfg.LoomCoinTransferGateway); err != nil {
				return err
			}

			if err := startLoomCoinGatewayFn(chainID, fnRegistry, cfg.LoomCoinTransferGateway, nodeSigner); err != nil {
				return err
			}

			if err := startPlasmaOracle(chainID, cfg.PlasmaCash); err != nil {
				return err
			}

			if err := startDPOSv2Oracle(chainID, cfg.DPOSv2OracleConfig); err != nil {
				return err
			}

			backend.RunForever()
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfg.Peers, "peers", "p", "", "peers")
	cmd.Flags().StringVar(&cfg.PersistentPeers, "persistent-peers", "", "persistent peers")
	cmd.Flags().StringVar(&abciServerAddr, "abci-server", "", "Serve ABCI app at specified address")
	cmd.Flags().Int64Var(&appHeight, "app-height", 0, "Start at the given block instead of the last block saved")
	return cmd
}

func recovery() {
	if r := recover(); r != nil {
		log.Error("caught RPC proxy exception, exiting", r)
		os.Exit(1)
	}
}

func startDPOSv2Oracle(chainID string, cfg *d2OracleCfg.OracleSerializableConfig) error {
	oracleCfg, err := d2OracleCfg.LoadSerializableConfig(chainID, cfg)
	if err != nil {
		return err
	}

	if !oracleCfg.Enabled {
		return nil
	}

	oracle := d2Oracle.NewOracle(oracleCfg)
	if err := oracle.Init(); err != nil {
		return err
	}

	oracle.Run()
	return nil
}

func startPlasmaOracle(chainID string, cfg *plasmaConfig.PlasmaCashSerializableConfig) error {
	plasmaCfg, err := plasmaConfig.LoadSerializableConfig(chainID, cfg)
	if err != nil {
		return err
	}

	if !plasmaCfg.OracleEnabled {
		return nil
	}

	oracle := plasmaOracle.NewOracle(plasmaCfg.OracleConfig)
	err = oracle.Init()
	if err != nil {
		return err
	}

	oracle.Run()

	return nil
}

func startGatewayFn(chainID string, fnRegistry fnConsensus.FnRegistry, cfg *tgateway.TransferGatewayConfig, nodeSigner glAuth.Signer) error {
	if !cfg.BatchSignFnConfig.Enabled {
		return nil
	}

	batchSignWithdrawalFn, err := tgateway.CreateBatchSignWithdrawalFn(false, chainID, fnRegistry, cfg, nodeSigner)
	if err != nil {
		return err
	}

	return fnRegistry.Set("batch_sign_withdrawal", batchSignWithdrawalFn)
}

func startLoomCoinGatewayFn(chainID string, fnRegistry fnConsensus.FnRegistry, cfg *tgateway.TransferGatewayConfig, nodeSigner glAuth.Signer) error {
	if !cfg.BatchSignFnConfig.Enabled {
		return nil
	}

	batchSignWithdrawalFn, err := tgateway.CreateBatchSignWithdrawalFn(true, chainID, fnRegistry, cfg, nodeSigner)
	if err != nil {
		return err
	}

	return fnRegistry.Set("loomcoin:batch_sign_withdrawal", batchSignWithdrawalFn)
}

func startLoomCoinGatewayOracle(chainID string, cfg *tgateway.TransferGatewayConfig) error {
	if !cfg.OracleEnabled {
		return nil
	}

	orc, err := tgateway.CreateLoomCoinOracle(cfg, chainID)
	if err != nil {
		return err
	}

	go orc.RunWithRecovery()
	return nil
}

func startGatewayOracle(chainID string, cfg *tgateway.TransferGatewayConfig) error {
	if !cfg.OracleEnabled {
		return nil
	}

	orc, err := tgateway.CreateOracle(cfg, chainID)
	if err != nil {
		return err
	}

	go orc.RunWithRecovery()
	return nil
}

func initDB(name, dir string) error {
	dbPath := filepath.Join(dir, name+".db")
	if util.FileExists(dbPath) {
		return errors.New("db already exists")
	}

	return nil
}

func destroyDB(name, dir string) error {
	dbPath := filepath.Join(dir, name+".db")
	return os.RemoveAll(dbPath)
}

func resetApp(cfg *config.Config) error {
	return destroyDB(cfg.DBName, cfg.RootPath())
}

func initApp(validator *loom.Validator, cfg *config.Config) error {
	gen, err := defaultGenesis(cfg, validator)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(cfg.GenesisPath(), os.O_EXCL|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(file)
	enc.SetIndent("", "    ")
	err = enc.Encode(gen)
	if err != nil {
		return err
	}

	err = initDB(cfg.DBName, cfg.RootPath())
	if err != nil {
		return err
	}

	return nil
}

func destroyApp(cfg *config.Config) error {
	err := util.IgnoreErrNotExists(os.Remove(cfg.GenesisPath()))
	if err != nil {
		return err
	}
	return resetApp(cfg)
}

func destroyReceiptsDB(cfg *config.Config) {
	if cfg.ReceiptsVersion == handler.ReceiptHandlerLevelDb {
		receptHandler := leveldb.LevelDbReceipts{}
		receptHandler.ClearData()
	}
}

func loadAppStore(cfg *config.Config, logger *loom.Logger, targetVersion int64) (store.VersionedKVStore, error) {
	db, err := cdb.LoadDB(
		cfg.DBBackend, cfg.DBName, cfg.RootPath(), cfg.DBBackendConfig.CacheSizeMegs, cfg.Metrics.Database,
	)
	if err != nil {
		return nil, err
	}

	if cfg.AppStore.CompactOnLoad {
		logger.Info("Compacting app store...")
		if err := db.Compact(); err != nil {
			// compaction erroring out may indicate larger issues with the db,
			// but for now let's try loading the app store anyway...
			logger.Error("Failed to compact app store", "DBName", cfg.DBName, "err", err)
		}
		logger.Info("Finished compacting app store")
	}

	var appStore store.VersionedKVStore
	if cfg.AppStore.Version == 1 { // TODO: cleanup these hardcoded numbers
		if cfg.AppStore.PruneInterval > int64(0) {
			logger.Info("Loading Pruning IAVL Store")
			appStore, err = store.NewPruningIAVLStore(db, store.PruningIAVLStoreConfig{
				MaxVersions: cfg.AppStore.MaxVersions,
				BatchSize:   cfg.AppStore.PruneBatchSize,
				Interval:    time.Duration(cfg.AppStore.PruneInterval) * time.Second,
				Logger:      logger,
			})
			if err != nil {
				return nil, err
			}
		} else {
			logger.Info("Loading IAVL Store")
			appStore, err = store.NewIAVLStore(db, cfg.AppStore.MaxVersions, targetVersion)
			if err != nil {
				return nil, err
			}
		}
	} else if cfg.AppStore.Version == 2 {
		logger.Info("Loading MultiReaderIAVL Store")
		valueDB, err := cdb.LoadDB(
			cfg.AppStore.LatestStateDBBackend, cfg.AppStore.LatestStateDBName, cfg.RootPath(),
			cfg.DBBackendConfig.CacheSizeMegs, cfg.Metrics.Database,
		)
		if err != nil {
			return nil, err
		}
		appStore, err = store.NewMultiReaderIAVLStore(db, valueDB, cfg.AppStore)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("Invalid AppStore.Version config setting")
	}

	if cfg.LogStateDB {
		appStore, err = store.NewLogStore(appStore)
		if err != nil {
			return nil, err
		}
	}

	// NOTE: Shouldn't wrap the MultiReaderIAVLStore in a CachingStore yet, otherwise the
	//       MultiReaderIAVLStore loses its advantages.
	if cfg.CachingStoreConfig.CachingEnabled &&
		((cfg.AppStore.Version == 1) || cfg.CachingStoreConfig.DebugForceEnable) {
		appStore, err = store.NewCachingStore(appStore, cfg.CachingStoreConfig)
		if err != nil {
			return nil, err
		}
		logger.Info("CachingStore enabled")
	}

	return appStore, nil
}

func loadEventStore(cfg *config.Config, logger *loom.Logger) (store.EventStore, error) {
	eventStoreCfg := cfg.EventStore
	db, err := cdb.LoadDB(
		eventStoreCfg.DBBackend, eventStoreCfg.DBName, cfg.RootPath(),
		20, //TODO do we want a seperate cache config for eventstore?,
		cfg.Metrics.Database,
	)
	if err != nil {
		return nil, err
	}

	var eventStore store.EventStore
	eventStore = store.NewKVEventStore(db)
	return eventStore, nil
}

func loadApp(chainID string, cfg *config.Config, loader plugin.Loader, b backend.Backend, appHeight int64) (*loomchain.Application, error) {
	logger := log.Root

	appStore, err := loadAppStore(cfg, log.Default, appHeight)
	if err != nil {
		return nil, err
	}

	var eventStore store.EventStore
	var eventDispatcher loomchain.EventDispatcher
	switch cfg.EventDispatcher.Dispatcher {
	case events.DispatcherDBIndexer:
		logger.Info("Using DB indexer event dispatcher")
		eventStore, err = loadEventStore(cfg, log.Default)
		if err != nil {
			return nil, err
		}
		eventDispatcher = events.NewDBIndexerEventDispatcher(eventStore)

	case events.DispatcherRedis:
		uri := cfg.EventDispatcher.Redis.URI
		logger.Info("Using Redis event dispatcher", "uri", uri)
		eventDispatcher, err = events.NewRedisEventDispatcher(uri)
		if err != nil {
			return nil, err
		}
	case events.DispatcherLog:
		logger.Info("Using simple log event dispatcher")
		eventDispatcher = events.NewLogEventDispatcher()
	default:
		return nil, fmt.Errorf("invalid event dispatcher %s", cfg.EventDispatcher.Dispatcher)
	}

	var eventHandler loomchain.EventHandler = loomchain.NewDefaultEventHandler(eventDispatcher)
	if cfg.Metrics.EventHandling {
		eventHandler = loomchain.NewInstrumentingEventHandler(eventHandler)
	}

	// TODO: It shouldn't be possible to change the registry version via config after the first run,
	//       changing it from that point on should require a special upgrade tx that stores the
	//       new version in the app store.
	regVer, err := registry.RegistryVersionFromInt(cfg.RegistryVersion)
	if err != nil {
		return nil, err
	}
	createRegistry, err := registry.NewRegistryFactory(regVer)
	if err != nil {
		return nil, err
	}

	receiptHandlerProvider := receipts.NewReceiptHandlerProvider(eventHandler, func(blockHeight int64) (handler.ReceiptHandlerVersion, uint64, error) {
		receiptVer, err := handler.ReceiptHandlerVersionFromInt(replay.OverrideConfig(cfg, blockHeight).ReceiptsVersion)
		if err != nil {
			return 0, 0, errors.Wrap(err, "failed to resolve receipt handler version")
		}
		return receiptVer, cfg.EVMPersistentTxReceiptsMax, nil
	})

	var newABMFactory plugin.NewAccountBalanceManagerFactoryFunc
	if evm.EVMEnabled && cfg.EVMAccountsEnabled {
		newABMFactory = plugin.NewAccountBalanceManagerFactory
	}

	vmManager := vm.NewManager()
	vmManager.Register(vm.VMType_PLUGIN, func(state loomchain.State) (vm.VM, error) {
		receiptReader, err := receiptHandlerProvider.ReaderAt(state.Block().Height)
		if err != nil {
			return nil, err
		}
		receiptWriter, err := receiptHandlerProvider.WriterAt(state.Block().Height)
		if err != nil {
			return nil, err
		}
		return plugin.NewPluginVM(
			loader,
			state,
			createRegistry(state),
			eventHandler,
			log.Default,
			newABMFactory,
			receiptWriter,
			receiptReader,
		), nil
	})

	if evm.EVMEnabled {
		vmManager.Register(vm.VMType_EVM, func(state loomchain.State) (vm.VM, error) {
			var createABM evm.AccountBalanceManagerFactoryFunc
			var err error

			receiptReader, err := receiptHandlerProvider.ReaderAt(state.Block().Height)
			if err != nil {
				return nil, err
			}
			receiptWriter, err := receiptHandlerProvider.WriterAt(state.Block().Height)
			if err != nil {
				return nil, err
			}

			if newABMFactory != nil {
				pvm := plugin.NewPluginVM(
					loader,
					state,
					createRegistry(state),
					eventHandler,
					log.Default,
					newABMFactory,
					receiptWriter,
					receiptReader,
				)
				createABM, err = newABMFactory(pvm)
				if err != nil {
					return nil, err
				}
			}
			return evm.NewLoomVm(state, eventHandler, receiptWriter, createABM, cfg.EVMDebugEnabled), nil
		})
	}
	evm.LogEthDbBatch = cfg.LogEthDbBatch

	deployTxHandler := &vm.DeployTxHandler{
		Manager:        vmManager,
		CreateRegistry: createRegistry,
	}

	callTxHandler := &vm.CallTxHandler{
		Manager: vmManager,
	}

	gen, err := config.ReadGenesis(cfg.GenesisPath())
	if err != nil {
		return nil, err
	}

	rootAddr := loom.RootAddress(chainID)
	init := func(state loomchain.State) error {
		registry := createRegistry(state)
		evm.AddLoomPrecompiles()
		for i, contractCfg := range gen.Contracts {
			err := deployContract(
				state,
				contractCfg,
				vmManager,
				rootAddr,
				registry,
				logger,
				i,
			)
			if err != nil {
				return errors.Wrapf(err, "deploying contract: %s", contractCfg.Name)
			}
		}
		return nil
	}

	router := loomchain.NewTxRouter()

	isEvmTx := func(txID uint32, state loomchain.State, txBytes []byte, isCheckTx bool) bool {
		var msg vm.MessageTx
		err := proto.Unmarshal(txBytes, &msg)
		if err != nil {
			return false
		}

		switch txID {
		case 1:
			var tx vm.DeployTx
			err = proto.Unmarshal(msg.Data, &tx)
			if err != nil {
				// In case of error, let's give safest response,
				// let's TxHandler down the line, handle it.
				return false
			}
			return tx.VmType == vm.VMType_EVM
		case 2:
			var tx vm.CallTx
			err = proto.Unmarshal(msg.Data, &tx)
			if err != nil {
				// In case of error, let's give safest response,
				// let's TxHandler down the line, handle it.
				return false
			}
			return tx.VmType == vm.VMType_EVM
		default:
			return false
		}
	}

	router.HandleDeliverTx(1, loomchain.GeneratePassthroughRouteHandler(deployTxHandler))
	router.HandleDeliverTx(2, loomchain.GeneratePassthroughRouteHandler(callTxHandler))

	// TODO: Write this in more elegant way
	router.HandleCheckTx(1, loomchain.GenerateConditionalRouteHandler(isEvmTx, loomchain.NoopTxHandler, deployTxHandler))
	router.HandleCheckTx(2, loomchain.GenerateConditionalRouteHandler(isEvmTx, loomchain.NoopTxHandler, callTxHandler))

	txMiddleWare := []loomchain.TxMiddleware{
		loomchain.LogTxMiddleware,
		loomchain.RecoveryTxMiddleware,
	}

	txMiddleWare = append(txMiddleWare, auth.NewChainConfigMiddleware(
		cfg.Auth,
		getContractCtx("addressmapper", vmManager),
	))

	createKarmaContractCtx := getContractCtx("karma", vmManager)

	if cfg.Karma.Enabled {
		txMiddleWare = append(txMiddleWare, throttle.GetKarmaMiddleWare(
			cfg.Karma.Enabled,
			cfg.Karma.MaxCallCount,
			cfg.Karma.SessionDuration,
			createKarmaContractCtx,
		))
	}

	createContractUpkeepHandler := func(state loomchain.State) (loomchain.KarmaHandler, error) {
		// TODO: This setting should be part of the config stored within the Karma contract itself,
		//       that will allow us to switch the upkeep on & off via a tx.
		if !cfg.Karma.UpkeepEnabled {
			return nil, nil
		}
		karmaContractCtx, err := createKarmaContractCtx(state)
		if err != nil {
			// Contract upkeep functionality depends on the Karma contract, so this feature will
			// remain disabled if the Karma contract hasn't been deployed yet.
			if err == regcommon.ErrNotFound {
				return nil, nil
			}
			return nil, err
		}
		return karma_handler.NewKarmaHandler(karmaContractCtx), nil
	}

	txMiddleWare = append(txMiddleWare, auth.NonceTxMiddleware)

	oracle, err := loom.ParseAddress(cfg.Oracle)
	if err != nil {
		oracle = loom.Address{}
	}
	deployerAddressList, err := cfg.TxLimiter.DeployerAddresses()
	if err != nil {
		return nil, err
	}
	deployerAddressList = append(deployerAddressList, oracle)

	originHandler := throttle.NewOriginValidator(
		uint64(cfg.TxLimiter.CallSessionDuration),
		deployerAddressList,
		cfg.TxLimiter.LimitDeploys,
		cfg.TxLimiter.LimitCalls,
	)

	// Technically ThrottleTxMiddleWare has been replaced by OriginHandler but to replay a couple
	// of old PlasmaChain production blocks correctly we have to keep this middleware around.
	// TODO: Implement height-based middleware overrides so this middleware is only activated for
	//       two blocks in PlasmaChain builds.
	txMiddleWare = append(txMiddleWare, throttle.GetThrottleTxMiddleWare(
		func(blockHeight int64) bool {
			return replay.OverrideConfig(cfg, blockHeight).DeployEnabled
		},
		func(blockHeight int64) bool {
			return replay.OverrideConfig(cfg, blockHeight).CallEnabled
		},
		oracle,
	))

	if cfg.GoContractDeployerWhitelist.Enabled {
		goDeployers, err := cfg.GoContractDeployerWhitelist.DeployerAddresses(chainID)
		if err != nil {
			return nil, errors.Wrapf(err, "getting list of users allowed go deploys")
		}
		txMiddleWare = append(txMiddleWare, throttle.GetGoDeployTxMiddleWare(goDeployers))
	}

	txMiddleWare = append(txMiddleWare, loomchain.NewInstrumentingTxMiddleware())

	createValidatorsManager := func(state loomchain.State) (loomchain.ValidatorsManager, error) {
		if cfg.DPOSVersion != 2 {
			return plugin.NewNoopValidatorsManager(), nil
		}
		pvm, err := vmManager.InitVM(vm.VMType_PLUGIN, state)
		if err != nil {
			return nil, err
		}
		return plugin.NewValidatorsManager(pvm.(*plugin.PluginVM))
	}

	createChainConfigManager := func(state loomchain.State) (loomchain.ChainConfigManager, error) {
		if !cfg.ChainConfig.ContractEnabled {
			return nil, nil
		}
		pvm, err := vmManager.InitVM(vm.VMType_PLUGIN, state)
		if err != nil {
			return nil, err
		}

		m, err := plugin.NewChainConfigManager(pvm.(*plugin.PluginVM), state)
		if err != nil {
			// This feature will remain disabled until the ChainConfig contract is deployed
			if err == plugin.ErrChainConfigContractNotFound {
				return nil, nil
			}
			return nil, err
		}
		return m, nil
	}

	postCommitMiddlewares := []loomchain.PostCommitMiddleware{
		loomchain.LogPostCommitMiddleware,
		auth.NonceTxPostNonceMiddleware,
	}
	if !cfg.Karma.Enabled && cfg.Karma.UpkeepEnabled {
		logger.Info("Karma disabled, upkeep enabled ignored")
	}

	return &loomchain.Application{
		Store: appStore,
		Init:  init,
		TxHandler: loomchain.MiddlewareTxHandler(
			txMiddleWare,
			router,
			postCommitMiddlewares,
		),
		EventHandler:                eventHandler,
		ReceiptHandlerProvider:      receiptHandlerProvider,
		CreateValidatorManager:      createValidatorsManager,
		CreateChainConfigManager:    createChainConfigManager,
		CreateContractUpkeepHandler: createContractUpkeepHandler,
		OriginHandler:               &originHandler,
		EventStore:                  eventStore,
	}, nil
}

func deployContract(
	state loomchain.State,
	contractCfg config.ContractConfig,
	vmManager *vm.Manager,
	rootAddr loom.Address,
	registry regcommon.Registry,
	logger log.TMLogger,
	index int,
) error {
	vmType := contractCfg.VMType()
	vm, err := vmManager.InitVM(vmType, state)
	if err != nil {
		return err
	}

	loader := codeLoaders[contractCfg.Format]
	initCode, err := loader.LoadContractCode(
		contractCfg.Location,
		contractCfg.Init,
	)
	if err != nil {
		return err
	}

	callerAddr := plugin.CreateAddress(rootAddr, uint64(index))
	_, addr, err := vm.Create(callerAddr, initCode, loom.NewBigUIntFromInt(0))
	if err != nil {
		return err
	}

	err = registry.Register(contractCfg.Name, addr, addr)
	if err != nil {
		return err
	}

	logger.Info("Deployed contract",
		"vm", contractCfg.VMTypeName,
		"location", contractCfg.Location,
		"name", contractCfg.Name,
		"address", addr,
	)
	return nil
}

type contextFactory func(state loomchain.State) (contractpb.Context, error)

func getContractCtx(pluginName string, vmManager *vm.Manager) contextFactory {
	return func(state loomchain.State) (contractpb.Context, error) {
		pvm, err := vmManager.InitVM(vm.VMType_PLUGIN, state)
		if err != nil {
			return nil, err
		}
		return plugin.NewInternalContractContext(pluginName, pvm.(*plugin.PluginVM))
	}
}

func initBackend(cfg *config.Config, abciServerAddr string, fnRegistry fnConsensus.FnRegistry) backend.Backend {
	ovCfg := &backend.OverrideConfig{
		LogLevel:                 cfg.BlockchainLogLevel,
		Peers:                    cfg.Peers,
		PersistentPeers:          cfg.PersistentPeers,
		ChainID:                  cfg.ChainID,
		RPCListenAddress:         cfg.RPCListenAddress,
		RPCProxyPort:             cfg.RPCProxyPort,
		CreateEmptyBlocks:        cfg.CreateEmptyBlocks,
		HsmConfig:                cfg.HsmConfig,
		FnConsensusReactorConfig: cfg.FnConsensus.Reactor,
	}
	return &backend.TendermintBackend{
		RootPath:    path.Join(cfg.RootPath(), "chaindata"),
		OverrideCfg: ovCfg,
		SocketPath:  abciServerAddr,
		FnRegistry:  fnRegistry,
	}
}

func initQueryService(
	app *loomchain.Application, chainID string, cfg *config.Config, loader plugin.Loader,
	receiptHandlerProvider loomchain.ReceiptHandlerProvider,
) error {
	// metrics
	fieldKeys := []string{"method", "error"}
	requestCount := kitprometheus.NewCounterFrom(stdprometheus.CounterOpts{
		Namespace: "loomchain",
		Subsystem: "query_service",
		Name:      "request_count",
		Help:      "Number of requests received.",
	}, fieldKeys)
	requestLatency := kitprometheus.NewSummaryFrom(stdprometheus.SummaryOpts{
		Namespace: "loomchain",
		Subsystem: "query_service",
		Name:      "request_latency_microseconds",
		Help:      "Total duration of requests in microseconds.",
	}, fieldKeys)

	regVer, err := registry.RegistryVersionFromInt(cfg.RegistryVersion)
	if err != nil {
		return err
	}
	createRegistry, err := registry.NewRegistryFactory(regVer)
	if err != nil {
		return err
	}

	var newABMFactory plugin.NewAccountBalanceManagerFactoryFunc
	if evm.EVMEnabled && cfg.EVMAccountsEnabled {
		newABMFactory = plugin.NewAccountBalanceManagerFactory
	}

	blockstore, err := store.NewBlockStore(cfg.BlockStore)
	if err != nil {
		return err
	}

	qs := &rpc.QueryServer{
		StateProvider:          app,
		ChainID:                chainID,
		Loader:                 loader,
		Subscriptions:          app.EventHandler.SubscriptionSet(),
		EthSubscriptions:       app.EventHandler.EthSubscriptionSet(),
		EthPolls:               *polls.NewEthSubscriptions(),
		CreateRegistry:         createRegistry,
		NewABMFactory:          newABMFactory,
		ReceiptHandlerProvider: receiptHandlerProvider,
		RPCListenAddress:       cfg.RPCListenAddress,
		BlockStore:             blockstore,
		EventStore:             app.EventStore,
		AuthCfg:                cfg.Auth,
	}
	bus := &rpc.QueryEventBus{
		Subs:    *app.EventHandler.SubscriptionSet(),
		EthSubs: *app.EventHandler.EthSubscriptionSet(),
	}
	// query service
	var qsvc rpc.QueryService
	{
		qsvc = qs
		qsvc = rpc.NewInstrumentingMiddleWare(requestCount, requestLatency, qsvc)
	}
	logger := log.Root.With("module", "query-server")
	err = rpc.RPCServer(qsvc, logger, bus, cfg.RPCBindAddress, cfg.UnsafeRPCEnabled, cfg.UnsafeRPCBindAddress)
	if err != nil {
		return err
	}

	return nil
}

func main() {
	karmaCmd := cli.ContractCallCommand(KarmaContractName)
	addressMappingCmd := cli.ContractCallCommand(AddressMapperName)
	callCommand := cli.ContractCallCommand("")
	dposCmd := cli.ContractCallCommand("dpos")
	commands.AddDPOSV2(dposCmd)

	resolveCmd := cli.ContractCallCommand("resolve")
	commands.AddGeneralCommands(resolveCmd)

	unsafeCmd := cli.ContractCallCommand("unsafe")
	commands.AddUnsafeCommands(unsafeCmd)

	commands.Add(callCommand)
	RootCmd.AddCommand(
		newVersionCommand(),
		newEnvCommand(),
		newInitCommand(),
		newResetCommand(),
		newRunCommand(),
		newSpinCommand(),
		newDeployCommand(),
		newDeployGoCommand(),
		callCommand,
		newGenKeyCommand(),
		newYubiHsmCommand(),
		newNodeKeyCommand(),
		newStaticCallCommand(), //Depreciate
		newGetBlocksByNumber(),
		karmaCmd,
		addressMappingCmd,
		gatewaycmd.NewGatewayCommand(),
		dbcmd.NewDBCommand(),
		newCallEvmCommand(), //Depreciate
		dposCmd,
		resolveCmd,
		unsafeCmd,
		commands.GetMapping(),
		commands.ListMapping(),
		staking.NewStakingCommand(),
		chainconfig.NewChainCfgCommand(),
		dbg.NewDebugCommand(),
	)
	AddKarmaMethods(karmaCmd)
	AddAddressMappingMethods(addressMappingCmd)

	err := RootCmd.Execute()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
