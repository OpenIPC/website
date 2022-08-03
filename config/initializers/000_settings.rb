require "resolv"

IP_ADDRESS_FORMAT=Regexp.union(Resolv::IPv4::Regex, Resolv::IPv6::Regex)
MAC_ADDRESS_FORMAT=/\A([0-9a-fA-F]{2}[:-]){5}[0-9a-fA-F]{2}\z/
