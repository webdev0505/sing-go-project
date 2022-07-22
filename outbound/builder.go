package outbound

import (
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
)

func New(router adapter.Router, logger log.ContextLogger, options option.Outbound) (adapter.Outbound, error) {
	if common.IsEmptyByEquals(options) {
		return nil, E.New("empty outbound config")
	}
	switch options.Type {
	case C.TypeDirect:
		return NewDirect(router, logger, options.Tag, options.DirectOptions), nil
	case C.TypeBlock:
		return NewBlock(logger, options.Tag), nil
	case C.TypeSocks:
		return NewSocks(router, logger, options.Tag, options.SocksOptions)
	case C.TypeHTTP:
		return NewHTTP(router, logger, options.Tag, options.HTTPOptions)
	case C.TypeShadowsocks:
		return NewShadowsocks(router, logger, options.Tag, options.ShadowsocksOptions)
	case C.TypeVMess:
		return NewVMess(router, logger, options.Tag, options.VMessOptions)
	case C.TypeSelector:
		return NewSelector(router, logger, options.Tag, options.SelectorOptions)
	case C.TypeURLTest:
		return NewURLTest(router, logger, options.Tag, options.URLTestOptions)
	default:
		return nil, E.New("unknown outbound type: ", options.Type)
	}
}
