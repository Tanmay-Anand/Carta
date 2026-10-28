package io.github.tanmayanand.carta

import android.content.Context
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.NetworkCapabilities
import android.net.wifi.WifiManager
import android.os.Build
import org.json.JSONArray
import org.json.JSONObject
import java.net.Inet4Address
import java.net.InetAddress
import java.net.NetworkInterface

/**
 * Tier P on Android: work out where the phone is standing, with the platform's
 * own APIs, before a single packet is sent.
 *
 * The desktop CLI does this by running `route`, `ipconfig` and `arp`, none of
 * which an Android app may run. The result here is the same profile, as JSON,
 * handed to the Go core — plus the confounders that only this platform knows
 * about, so the core never has to ask what platform it is on.
 */
class OrientationProvider(private val context: Context) {

    sealed interface Result {
        data class Ready(val profileJson: String) : Result
        data class Refused(val kind: Refusal, val reason: String) : Result
    }

    /** Why the phone will not survey from here; each has its own next step. */
    enum class Refusal { OFFLINE, VPN, NOT_LOCAL, NO_IPV4 }

    fun read(): Result {
        val cm = context.getSystemService(ConnectivityManager::class.java)
        val network = cm.activeNetwork
            ?: return Result.Refused(Refusal.OFFLINE, "The phone is not connected to any network.")
        val caps = cm.getNetworkCapabilities(network)
            ?: return Result.Refused(Refusal.OFFLINE, "The active network reported no capabilities.")
        val link = cm.getLinkProperties(network)
            ?: return Result.Refused(Refusal.OFFLINE, "The active network reported no addresses.")

        // Refused, not merely noted. Go opens its sockets directly, outside the
        // hooks Android uses to steer an app's traffic, so whether a probe to a
        // LAN address leaves over Wi-Fi or into the tunnel depends on the VPN's
        // routes — and a survey that does not know which network it measured
        // has measured nothing.
        @Suppress("DEPRECATION") // allNetworks: the only way to see a VPN that is not the default network
        val vpn = caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN) ||
            cm.allNetworks.any { cm.getNetworkCapabilities(it)?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true }
        if (vpn) {
            return Result.Refused(Refusal.VPN, "Carta cannot tell whether its probes would go out over Wi-Fi or into the VPN's tunnel, and a survey that does not know which network it measured has measured nothing.")
        }
        val wifi = caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)
        if (!wifi && !caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET)) {
            return Result.Refused(Refusal.NOT_LOCAL, "The phone is using mobile data. Carta surveys the local network it is attached to, and will not probe a mobile carrier's network.")
        }

        val v4 = link.linkAddresses.firstOrNull { it.address is Inet4Address }
            ?: return Result.Refused(Refusal.NO_IPV4, "This network gave the phone no IPv4 address, and Carta surveys IPv4 only.")
        val ip = v4.address as Inet4Address

        val profile = JSONObject()
            .put("host", "${Build.MANUFACTURER} ${Build.MODEL}")
            .put("os", "android ${Build.VERSION.RELEASE} (API ${Build.VERSION.SDK_INT})")
            .put("interface", link.interfaceName ?: "")
            .put("ip", ip.hostAddress)
            .put("prefix_len", v4.prefixLength)
            .put("gateway", gateway(link))
            .put("resolvers", JSONArray(resolvers(link, ip, v4.prefixLength)))
            .put("search", JSONArray(link.domains?.split(' ', ',')?.filter { it.isNotBlank() } ?: emptyList<String>()))
            .put("dhcp_server", dhcpServer(link))
            // The SSID needs the location permission, and is not an identity
            // anyway: networks share names and one network has many APs.
            .put("wireless", "")
            .put("confounders", JSONArray(confounders(link, wifi)))
            .put("interfaces", interfaces())
        return Result.Ready(profile.toString())
    }

    private fun gateway(link: LinkProperties): String =
        link.routes.firstOrNull { it.isDefaultRoute && it.gateway is Inet4Address }?.gateway?.hostAddress ?: ""

    /**
     * The network's own resolvers, the ones on this prefix first. Reverse DNS
     * asks them in this order, directly — with Private DNS on, the system
     * resolver would send PTR queries over TLS to a public resolver that knows
     * nothing about 192.168.x.x.
     */
    private fun resolvers(link: LinkProperties, ip: Inet4Address, prefixLen: Int): List<String> {
        fun onPrefix(a: InetAddress) = a is Inet4Address && samePrefix(a, ip, prefixLen)
        return link.dnsServers
            .sortedBy { a -> if (onPrefix(a)) 0 else if (a is Inet4Address) 1 else 2 }
            .mapNotNull { it.hostAddress }
    }

    private fun dhcpServer(link: LinkProperties): String {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            link.dhcpServerAddress?.let { return it.hostAddress ?: "" }
        }
        @Suppress("DEPRECATION") // DhcpInfo: the only source below API 30
        val raw = context.applicationContext.getSystemService(WifiManager::class.java)?.dhcpInfo?.serverAddress ?: 0
        if (raw == 0) return ""
        // DhcpInfo packs the address little-endian into an int.
        return listOf(raw and 0xff, raw shr 8 and 0xff, raw shr 16 and 0xff, raw shr 24 and 0xff).joinToString(".")
    }

    /** What makes this phone a biased instrument, in the words the survey will print. */
    private fun confounders(link: LinkProperties, wifi: Boolean): List<String> {
        val out = mutableListOf(
            "Android hides the ARP cache and other devices' MAC addresses from apps: no device was confirmed at layer 2, " +
                "no vendor was looked up from a hardware address, and drift can match devices only by name or address",
        )
        if (wifi) {
            out += "surveyed over Wi-Fi: an access point with client isolation (common on guest networks) hides other wireless devices from this one"
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P && link.isPrivateDnsActive) {
            val name = link.privateDnsServerName?.let { " ($it)" } ?: ""
            out += "Private DNS is on$name; reverse lookups were sent to the network's own DNS servers directly"
        }
        link.httpProxy?.let { out += "a system HTTP proxy is configured (${it.host}:${it.port}); the survey's own traffic does not use it" }
        return out
    }

    private fun interfaces(): JSONArray {
        val out = JSONArray()
        try {
            for (ifc in NetworkInterface.getNetworkInterfaces()?.toList().orEmpty()) {
                val addrs = ifc.interfaceAddresses.mapNotNull { a -> a.address?.hostAddress?.let { "$it/${a.networkPrefixLength}" } }
                out.put(JSONObject().put("name", ifc.name).put("addrs", JSONArray(addrs)).put("up", ifc.isUp))
            }
        } catch (_: Exception) {
            // Recorded for the as-built document only; a phone that will not
            // list its interfaces still knows the one that matters.
        }
        return out
    }

    private fun samePrefix(a: Inet4Address, b: Inet4Address, prefixLen: Int): Boolean {
        val mask = if (prefixLen == 0) 0 else -1 shl (32 - prefixLen)
        return (toInt(a) and mask) == (toInt(b) and mask)
    }

    private fun toInt(a: Inet4Address): Int = a.address.fold(0) { acc, byte -> (acc shl 8) or (byte.toInt() and 0xff) }
}
