package io.github.tanmayanand.carta

import android.content.Context
import io.github.tanmayanand.carta.core.Core
import org.json.JSONObject
import java.io.File

/**
 * Surveys kept in app-private storage, one snapshot per file, in the same JSON
 * the CLI writes — a survey copied off the phone opens in `carta ui`.
 *
 * Each snapshot has a small fingerprint beside it so the list the operator
 * picks a drift baseline from does not reparse every survey ever taken.
 */
class SurveyStore(context: Context) {
    private val dir = File(context.filesDir, "surveys").apply { mkdirs() }

    /** What the baseline list shows. None of it is an identity: see Core.fingerprint. */
    data class Entry(
        val id: String,
        val startedAt: String,
        val prefix: String,
        val gateway: String,
        val dhcpServer: String,
        val resolvers: List<String>,
        val instrument: String,
        val devices: Int,
        val cutShort: String,
    )

    /** Saves a snapshot and returns its entry. */
    fun save(snapshot: String): Entry {
        val fp = Core.fingerprint(snapshot)
        val entry = parse(fp)
        File(dir, "${entry.id}.json").writeText(snapshot)
        File(dir, "${entry.id}.fp.json").writeText(fp)
        return entry
    }

    fun read(id: String): String? = File(dir, "$id.json").takeIf { it.exists() }?.readText()

    /** Newest first. */
    fun list(): List<Entry> =
        dir.listFiles { f -> f.name.endsWith(".fp.json") }.orEmpty()
            .mapNotNull { runCatching { parse(it.readText()) }.getOrNull() }
            .sortedByDescending { it.id }

    fun latest(): Pair<Entry, String>? = list().firstNotNullOfOrNull { e -> read(e.id)?.let { e to it } }

    private fun parse(fp: String): Entry {
        val o = JSONObject(fp)
        val resolvers = o.optJSONArray("resolvers")
        return Entry(
            id = o.getString("survey_id"),
            startedAt = o.optString("started_at"),
            prefix = o.optString("prefix"),
            gateway = o.optString("gateway"),
            dhcpServer = o.optString("dhcp_server"),
            resolvers = (0 until (resolvers?.length() ?: 0)).map { resolvers!!.getString(it) },
            instrument = o.optString("instrument"),
            devices = o.optInt("devices"),
            cutShort = o.optString("cut_short"),
        )
    }
}
