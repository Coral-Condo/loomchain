package benchmarks

import (
	"context"
	"encoding/json"

	"github.com/gogo/protobuf/proto"
	"github.com/loomnetwork/go-loom"
	ctypes "github.com/loomnetwork/go-loom/builtin/types/coin"
	dtypes "github.com/loomnetwork/go-loom/builtin/types/dposv3"
	"github.com/loomnetwork/loomchain"
	"github.com/loomnetwork/loomchain/builtin/plugins/dposv3"
	"github.com/loomnetwork/loomchain/builtin/plugins/coin"
	pl "github.com/loomnetwork/go-loom/plugin"
	"github.com/loomnetwork/loomchain/plugin"
	"github.com/loomnetwork/loomchain/log"
	"github.com/loomnetwork/loomchain/registry"
	"github.com/loomnetwork/loomchain/registry/factory"
	"github.com/loomnetwork/loomchain/store"
	"github.com/loomnetwork/loomchain/vm"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/db"
)
var (
	chainID         = "default"
	startTime int64 = 100000
)

type FakeStateContext struct {
	pl.FakeContext
	state    loomchain.State
	registry registry.Registry
	VM       vm.VM
}

func CreateFakeStateContext(state loomchain.State, reg registry.Registry, caller, address loom.Address, pluginVm vm.VM) *FakeStateContext {
	fakeContext := pl.CreateFakeContext(caller, address).WithBlock(loom.BlockHeader{
		ChainID: chainID,
		Time:    startTime,
	})

	return &FakeStateContext{
		FakeContext: *fakeContext,
		state:       state.WithPrefix(loom.DataPrefix(address)),
		registry:    reg,
		VM:          pluginVm,
	}
}

func MockStateWithDposAndCoin(dposInit *dtypes.DPOSInitRequest, coinInit *ctypes.InitRequest, appDb db.DB) (loomchain.State, registry.Registry, vm.VM, error) {
	appStore, err := store.NewIAVLStore(appDb, 0, 0)
	if err != nil {
		return nil, nil, nil, err
	}
	header := abci.Header{}
	header.Height = int64(1)
	state := loomchain.NewStoreState(context.Background(), appStore, header, nil, nil)

	vmManager := vm.NewManager()
	createRegistry, err := factory.NewRegistryFactory(factory.RegistryV2)
	reg := createRegistry(state)
	if err != nil {
		return nil, nil, nil, err
	}
	loader := plugin.NewStaticLoader(dposv3.Contract, coin.Contract)
	vmManager.Register(vm.VMType_PLUGIN, func(state loomchain.State) (vm.VM, error) {
		return plugin.NewPluginVM(loader, state, reg, nil, log.Default, nil, nil, nil), nil
	})
	pluginVm, err := vmManager.InitVM(vm.VMType_PLUGIN, state)
	if err != nil {
		return nil, nil, nil, err
	}

	if dposInit != nil {
		dposCode, err := json.Marshal(dposInit)
		if err != nil {
			return nil, nil, nil, err
		}
		dposInitCode, err := LoadContractCode("dposV3:3.0.0", dposCode)
		if err != nil {
			return nil, nil, nil, err
		}
		callerAddr := plugin.CreateAddress(loom.RootAddress(chainID), uint64(0))
		_, dposAddr, err := pluginVm.Create(callerAddr, dposInitCode, loom.NewBigUIntFromInt(0))
		if err != nil {
			return nil, nil, nil, err
		}

		err = reg.Register("dposV3", dposAddr, dposAddr)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	if coinInit != nil {
		coinCode, err := json.Marshal(coinInit)
		if err != nil {
			return nil, nil, nil, err
		}
		coinInitCode, err := LoadContractCode("coin:1.0.0", coinCode)
		if err != nil {
			return nil, nil, nil, err
		}
		callerAddr := plugin.CreateAddress(loom.RootAddress(chainID), uint64(1))
		_, coinAddr, err := pluginVm.Create(callerAddr, coinInitCode, loom.NewBigUIntFromInt(0))
		if err != nil {
			return nil, nil, nil, err
		}
		err = reg.Register("coin", coinAddr, coinAddr)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return state, reg, pluginVm, nil
}

// copied from PluginCodeLoader.LoadContractCode maybe move PluginCodeLoader to separate package
func LoadContractCode(location string, init json.RawMessage) ([]byte, error) {
	body, err := init.MarshalJSON()
	if err != nil {
		return nil, err
	}

	req := &plugin.Request{
		ContentType: plugin.EncodingType_JSON,
		Body:        body,
	}

	input, err := proto.Marshal(req)
	if err != nil {
		return nil, err
	}

	pluginCode := &plugin.PluginCode{
		Name:  location,
		Input: input,
	}
	return proto.Marshal(pluginCode)
}
