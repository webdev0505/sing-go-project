package route

import (
	"context"
	"net/netip"
	"strings"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-dns"
	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"

	"golang.org/x/net/dns/dnsmessage"
)

func (r *Router) Exchange(ctx context.Context, message *dnsmessage.Message) (*dnsmessage.Message, error) {
	if len(message.Questions) > 0 {
		r.dnsLogger.DebugContext(ctx, "exchange ", formatDNSQuestion(message.Questions[0]))
	}
	ctx, transport := r.matchDNS(ctx)
	ctx, cancel := context.WithTimeout(ctx, C.DNSTimeout)
	defer cancel()
	response, err := r.dnsClient.Exchange(ctx, transport, message)
	if err != nil && len(message.Questions) > 0 {
		r.dnsLogger.ErrorContext(ctx, E.Cause(err, "exchange failed for ", message.Questions[0].Name.String()))
	}
	if response != nil {
		LogDNSAnswers(r.dnsLogger, ctx, message.Questions[0].Name.String(), response.Answers)
	}
	return response, err
}

func (r *Router) Lookup(ctx context.Context, domain string, strategy dns.DomainStrategy) ([]netip.Addr, error) {
	r.dnsLogger.DebugContext(ctx, "lookup domain ", domain)
	ctx, transport := r.matchDNS(ctx)
	ctx, cancel := context.WithTimeout(ctx, C.DNSTimeout)
	defer cancel()
	addrs, err := r.dnsClient.Lookup(ctx, transport, domain, strategy)
	if len(addrs) > 0 {
		r.logger.InfoContext(ctx, "lookup succeed for ", domain, ": ", strings.Join(F.MapToString(addrs), " "))
	} else {
		r.logger.ErrorContext(ctx, E.Cause(err, "lookup failed for ", domain))
	}
	return addrs, err
}

func (r *Router) LookupDefault(ctx context.Context, domain string) ([]netip.Addr, error) {
	return r.Lookup(ctx, domain, r.defaultDomainStrategy)
}

func LogDNSAnswers(logger log.ContextLogger, ctx context.Context, domain string, answers []dnsmessage.Resource) {
	for _, rawAnswer := range answers {
		var content string
		switch answer := rawAnswer.Body.(type) {
		case *dnsmessage.AResource:
			content = netip.AddrFrom4(answer.A).String()
		case *dnsmessage.NSResource:
			content = answer.NS.String()
		case *dnsmessage.CNAMEResource:
			content = answer.CNAME.String()
		case *dnsmessage.SOAResource:
			content = answer.MBox.String()
		case *dnsmessage.PTRResource:
			content = answer.PTR.String()
		case *dnsmessage.MXResource:
			content = answer.MX.String()
		case *dnsmessage.TXTResource:
			content = strings.Join(answer.TXT, " ")
		case *dnsmessage.AAAAResource:
			content = netip.AddrFrom16(answer.AAAA).String()
		case *dnsmessage.SRVResource:
			content = answer.Target.String()
		case *dnsmessage.UnknownResource:
			content = answer.Type.String()
		default:
			continue
		}
		rType := formatDNSType(rawAnswer.Header.Type)
		if rType == "" {
			logger.InfoContext(ctx, "exchanged ", domain, " ", rType)
		} else {
			logger.InfoContext(ctx, "exchanged ", domain, " ", rType, " ", content)
		}
	}
}

func formatDNSQuestion(question dnsmessage.Question) string {
	var qType string
	qType = question.Type.String()
	if len(qType) > 4 {
		qType = qType[4:]
	}
	var qClass string
	qClass = question.Class.String()
	if len(qClass) > 5 {
		qClass = qClass[5:]
	}
	return string(question.Name.Data[:question.Name.Length-1]) + " " + qType + " " + qClass
}

func formatDNSType(qType dnsmessage.Type) string {
	qTypeName := qType.String()
	if len(qTypeName) > 4 {
		return qTypeName[4:]
	} else {
		return F.ToString("unknown (type ", qTypeName, ")")
	}
}
