package io.github.tanmayanand.carta

import android.annotation.SuppressLint
import android.content.Intent
import android.content.res.Configuration
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import android.text.SpannableStringBuilder
import android.text.Spanned
import android.text.style.ForegroundColorSpan
import android.view.HapticFeedbackConstants
import android.view.LayoutInflater
import android.view.View
import android.view.WindowManager
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.LinearLayout
import android.widget.TextView
import androidx.activity.OnBackPressedCallback
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.updatePadding
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import androidx.webkit.WebViewAssetLoader
import com.google.android.material.bottomsheet.BottomSheetBehavior
import com.google.android.material.bottomsheet.BottomSheetDialog
import com.google.android.material.button.MaterialButton
import com.google.android.material.progressindicator.LinearProgressIndicator
import com.google.android.material.snackbar.Snackbar
import io.github.tanmayanand.carta.OrientationProvider.Refusal
import io.github.tanmayanand.carta.SurveyViewModel.Status
import kotlinx.coroutines.launch
import java.io.ByteArrayInputStream
import java.text.DateFormat
import java.time.OffsetDateTime
import java.util.Date

class MainActivity : AppCompatActivity() {

    private val model: SurveyViewModel by viewModels()

    private lateinit var root: View
    private lateinit var actions: View
    private lateinit var web: WebView
    private lateinit var empty: View
    private lateinit var refusal: View
    private lateinit var status: TextView
    private lateinit var tag: TextView
    private lateinit var progress: LinearProgressIndicator
    private lateinit var surveyButton: MaterialButton
    private lateinit var compareButton: MaterialButton
    private var shownGeneration = -1
    private var shownDriftEvent = 0
    private var lastStatus: Status? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        root = findViewById(R.id.root)
        ViewCompat.setOnApplyWindowInsetsListener(root) { v, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout())
            v.updatePadding(left = bars.left, top = bars.top, right = bars.right, bottom = bars.bottom)
            insets
        }

        actions = findViewById(R.id.actions)
        web = findViewById(R.id.web)
        empty = findViewById(R.id.empty)
        refusal = findViewById(R.id.refusal)
        status = findViewById(R.id.status)
        tag = findViewById(R.id.tag)
        progress = findViewById(R.id.progress)
        surveyButton = findViewById(R.id.survey)
        compareButton = findViewById(R.id.compare)

        setUpWebView()
        surveyButton.setOnClickListener { if (model.running) model.stop() else model.survey() }
        compareButton.setOnClickListener { pickBaseline() }
        findViewById<View>(R.id.refusalBack).setOnClickListener { model.dismissRefusal() }
        onBackPressedDispatcher.addCallback(this, backFoldsTheSheet)

        // The drift snackbar is a one-off; one already seen must not reappear
        // because the Activity was recreated.
        shownDriftEvent = savedInstanceState?.getInt(KEY_DRIFT_EVENT) ?: model.state.value.drift?.event ?: 0

        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.STARTED) {
                model.state.collect(::render)
            }
        }
    }

    /**
     * Back first folds what the page has open — the legend, then the sheet —
     * and only leaves the app when there is nothing left to fold. The page
     * answers through window.carta.collapse(); the app calls the page, never
     * the other way round.
     */
    private val backFoldsTheSheet = object : OnBackPressedCallback(true) {
        override fun handleOnBackPressed() {
            if (web.visibility != View.VISIBLE) return leave()
            web.evaluateJavascript("window.carta ? carta.collapse() : false") { folded ->
                if (folded != "true") leave()
            }
        }

        private fun leave() {
            isEnabled = false
            onBackPressedDispatcher.onBackPressed()
            isEnabled = true
        }
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        outState.putInt(KEY_DRIFT_EVENT, shownDriftEvent)
    }

    private fun render(state: SurveyViewModel.UiState) {
        val s = state.status
        feelTransition(lastStatus, s)
        lastStatus = s
        val busy = s is Status.Running || s is Status.Stopping
        surveyButton.setText(
            when (s) {
                is Status.Running -> R.string.cancel
                Status.Stopping -> R.string.status_stopping
                else -> R.string.survey
            },
        )
        surveyButton.isEnabled = s !is Status.Stopping
        compareButton.isEnabled = state.shown != null && !busy

        // A survey takes minutes and the phone must not sleep through it: the
        // Wi-Fi radio powers down with the screen on many devices.
        if (busy) window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        else window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)

        renderProgress(s)
        renderStatus(s, state.shown)

        val refused = s as? Status.Refused
        refusal.visibility = if (refused != null) View.VISIBLE else View.GONE
        web.visibility = if (refused == null && state.shown != null) View.VISIBLE else View.GONE
        empty.visibility = if (refused == null && state.shown == null) View.VISIBLE else View.GONE
        refused?.let { renderRefusal(it, hasSurvey = state.shown != null) }

        val newDrift = state.drift?.takeIf { it.event != shownDriftEvent }
        if (state.shown != null && state.generation != shownGeneration) {
            shownGeneration = state.generation
            web.loadUrl(if (newDrift != null) INDEX_DRIFT else INDEX)
        }
        newDrift?.let { d ->
            shownDriftEvent = d.event
            Snackbar.make(root, d.summary, Snackbar.LENGTH_LONG)
                .apply { if (portrait()) anchorView = actions }
                .setAction(R.string.compare_view) { web.evaluateJavascript("showTab('drift')", null) }
                .show()
        }
    }

    /**
     * One pulse when a survey finishes and a different one when Carta refuses
     * to survey: the two outcomes worth knowing about without looking. Only on
     * a change seen in this Activity, never on the first render after a
     * rotation. performHapticFeedback follows the phone's own touch-feedback
     * setting, so turning that off there turns this off too.
     */
    private fun feelTransition(before: Status?, now: Status) {
        if (before == null || Build.VERSION.SDK_INT < Build.VERSION_CODES.R) return
        val busyBefore = before is Status.Running || before is Status.Stopping
        when {
            busyBefore && now is Status.Done -> root.performHapticFeedback(HapticFeedbackConstants.CONFIRM)
            before !is Status.Refused && now is Status.Refused -> root.performHapticFeedback(HapticFeedbackConstants.REJECT)
        }
    }

    private fun renderProgress(s: Status) {
        val wantVisible = s is Status.Running || s is Status.Stopping
        val determinate = s is Status.Running && s.total > 0
        if (!wantVisible) {
            progress.visibility = View.INVISIBLE
            return
        }
        // The indicator refuses to switch mode while shown, so it is hidden
        // for the switch.
        if (progress.isIndeterminate == determinate) {
            progress.visibility = View.INVISIBLE
            progress.isIndeterminate = !determinate
        }
        if (s is Status.Running && determinate) {
            progress.max = s.total.toInt()
            progress.setProgressCompat(s.done.toInt(), false)
        }
        progress.visibility = View.VISIBLE
    }

    private fun renderStatus(s: Status, shown: SurveyStore.Entry?) {
        tag.visibility = View.GONE
        status.setTextColor(getColor(R.color.dim))
        status.text = when (s) {
            Status.Idle -> getString(R.string.status_idle)
            is Status.Running ->
                if (s.total > 0) getString(R.string.status_progress, stageName(s.stage), s.done, s.total)
                else getString(R.string.status_stage, stageName(s.stage))
            Status.Stopping -> getString(R.string.status_stopping)
            is Status.Done -> doneText(s.entry)
            // The refusal itself fills the content area; the strip keeps
            // describing the survey that is still on file.
            is Status.Refused -> shown?.let(::doneText) ?: getString(R.string.status_idle)
            is Status.Failed -> {
                status.setTextColor(getColor(R.color.risk))
                s.message
            }
        }
        val cut = (s as? Status.Done)?.entry ?: (s as? Status.Refused)?.let { shown }
        if (cut != null && cut.cutShort.isNotEmpty()) tag.visibility = View.VISIBLE
    }

    private fun doneText(e: SurveyStore.Entry): String =
        if (e.cutShort.isNotEmpty()) getString(R.string.status_cut_short, e.devices, e.prefix, stageName(e.cutShort))
        else getString(R.string.status_done, e.devices, e.prefix, formatTime(e.startedAt, DateFormat.getTimeInstance(DateFormat.SHORT)))

    private fun renderRefusal(r: Status.Refused, hasSurvey: Boolean) {
        val (title, next) = when (r.kind) {
            Refusal.VPN -> R.string.refusal_vpn_title to R.string.refusal_vpn_next
            Refusal.NOT_LOCAL -> R.string.refusal_not_local_title to R.string.refusal_not_local_next
            Refusal.NO_IPV4 -> R.string.refusal_no_ipv4_title to R.string.refusal_no_ipv4_next
            Refusal.OFFLINE -> R.string.refusal_offline_title to R.string.refusal_offline_next
        }
        findViewById<TextView>(R.id.refusalTitle).setText(title)
        findViewById<TextView>(R.id.refusalReason).text = r.reason
        findViewById<TextView>(R.id.refusalNext).setText(next)
        findViewById<View>(R.id.refusalBack).visibility = if (hasSurvey) View.VISIBLE else View.GONE
        findViewById<MaterialButton>(R.id.refusalAction).apply {
            if (r.kind == Refusal.VPN) {
                setText(R.string.refusal_vpn_action)
                setOnClickListener { openSettings(Intent(Settings.ACTION_VPN_SETTINGS)) }
            } else {
                setText(R.string.refusal_wifi_action)
                setOnClickListener {
                    openSettings(
                        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) Intent(Settings.Panel.ACTION_WIFI)
                        else Intent(Settings.ACTION_WIFI_SETTINGS),
                    )
                }
            }
        }
    }

    private fun openSettings(intent: Intent) {
        runCatching { startActivity(intent) }.onFailure { startActivity(Intent(Settings.ACTION_WIRELESS_SETTINGS)) }
    }

    private fun stageName(stage: String): String = when (stage) {
        "orient" -> getString(R.string.stage_orient)
        "listen" -> getString(R.string.stage_listen)
        "sweep" -> getString(R.string.stage_sweep)
        "probe" -> getString(R.string.stage_probe)
        "nbns" -> getString(R.string.stage_nbns)
        "rdns" -> getString(R.string.stage_rdns)
        "resolve" -> getString(R.string.stage_resolve)
        else -> stage
    }

    private fun portrait() = resources.configuration.orientation != Configuration.ORIENTATION_LANDSCAPE

    /**
     * The WebView shows the CLI's UI from the APK's assets and nothing else.
     *
     * Everything it renders came off an untrusted network — hostnames, banners
     * and mDNS names are whatever the devices chose to say. So: no file or
     * content access, no JavaScript bridge into the app, no navigation away
     * from the bundled pages, and a CSP in index.html as a backstop for the
     * escaping in app.js. (evaluateJavascript is the app calling the page,
     * never the page calling the app.)
     *
     * Text size: this WebView already scales its text with the system font
     * size (measured at font scale 2.0), so textZoom is left at its default
     * rather than applied a second time.
     */
    @SuppressLint("SetJavaScriptEnabled")
    private fun setUpWebView() {
        web.settings.apply {
            javaScriptEnabled = true
            allowFileAccess = false
            allowContentAccess = false
            domStorageEnabled = false
            setGeolocationEnabled(false)
            setSupportMultipleWindows(false)
        }
        web.setBackgroundColor(getColor(R.color.bg))

        // api/* is answered from memory, before the asset handler sees it. The
        // order matters: WebViewAssetLoader uses the first handler that answers.
        val loader = WebViewAssetLoader.Builder()
            .addPathHandler("/assets/api/") { path -> api(path) }
            .addPathHandler("/assets/", WebViewAssetLoader.AssetsPathHandler(this))
            .build()

        web.webViewClient = object : WebViewClient() {
            override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? =
                loader.shouldInterceptRequest(request.url) ?: blocked()

            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean =
                request.url.host != WebViewAssetLoader.DEFAULT_DOMAIN
        }
    }

    private fun api(path: String): WebResourceResponse {
        val body = when (path) {
            "survey" -> model.snapshot
            "drift" -> model.drift
            else -> null
        } ?: return notFound()
        // no-store: a new survey must replace the page's data on reload, never
        // be answered from the WebView's cache.
        return WebResourceResponse(
            "application/json", "utf-8", 200, "OK",
            mapOf("Cache-Control" to "no-store"),
            ByteArrayInputStream(body.toByteArray()),
        )
    }

    private fun notFound() = WebResourceResponse(
        "text/plain", "utf-8", 404, "Not Found", mapOf("Cache-Control" to "no-store"), ByteArrayInputStream(ByteArray(0)),
    )

    // Anything that is not one of the bundled pages is refused rather than
    // fetched: the UI never needs the network, and a page that tried would be
    // a page doing something it was not written to do.
    private fun blocked() = WebResourceResponse(
        "text/plain", "utf-8", 403, "Forbidden", emptyMap(), ByteArrayInputStream(ByteArray(0)),
    )

    /**
     * The baseline picker. Every field that differs from the survey on screen
     * is shown in the warning colour: a difference is evidence of a different
     * network, while a match proves nothing, and the hint says so.
     */
    private fun pickBaseline() {
        lifecycleScope.launch {
            val (current, options) = model.baselines()
            val sheet = BottomSheetDialog(this@MainActivity)
            val view = LayoutInflater.from(this@MainActivity).inflate(R.layout.sheet_baselines, null)
            val rows = view.findViewById<LinearLayout>(R.id.sheetRows)

            if (options.isEmpty()) view.findViewById<TextView>(R.id.sheetHint).setText(R.string.compare_none)
            for (e in options) {
                val row = LayoutInflater.from(this@MainActivity).inflate(R.layout.item_baseline, rows, false)
                row.findViewById<TextView>(R.id.rowWhen).text =
                    formatTime(e.startedAt, DateFormat.getDateTimeInstance(DateFormat.MEDIUM, DateFormat.SHORT))
                val (printed, spoken) = fingerprint(e, current)
                row.findViewById<TextView>(R.id.rowFingerprint).text = printed
                val tags = buildList {
                    if (e.cutShort.isNotEmpty()) add(getString(R.string.tag_stopped_early))
                    if (current != null && e.instrument != current.instrument) add(e.instrument.uppercase())
                }
                row.findViewById<TextView>(R.id.rowTag).apply {
                    visibility = if (tags.isEmpty()) View.GONE else View.VISIBLE
                    text = tags.joinToString(", ")
                }
                row.contentDescription = "${row.findViewById<TextView>(R.id.rowWhen).text}. $spoken. ${tags.joinToString(". ")}"
                row.setOnClickListener {
                    sheet.dismiss()
                    model.compareWith(e)
                }
                rows.addView(row)
            }

            view.findViewById<View>(R.id.sheetClear).apply {
                visibility = if (model.state.value.drift != null) View.VISIBLE else View.GONE
                setOnClickListener {
                    sheet.dismiss()
                    model.clearComparison()
                }
            }

            sheet.setContentView(view)
            // On a phone on its side the half-height state is a sliver; open
            // the sheet fully there.
            if (!portrait()) sheet.behavior.state = BottomSheetBehavior.STATE_EXPANDED
            sheet.behavior.skipCollapsed = !portrait()
            sheet.show()
        }
    }

    /** The fingerprint block, with fields that differ from [current] marked, and its spoken form. */
    private fun fingerprint(e: SurveyStore.Entry, current: SurveyStore.Entry?): Pair<CharSequence, String> {
        val out = SpannableStringBuilder()
        val spoken = mutableListOf<String>()
        val warn = getColor(R.color.warn)
        fun field(label: String, value: String, differs: Boolean) {
            if (value.isEmpty()) return
            if (out.isNotEmpty() && out.last() != '\n') out.append("  ")
            out.append(label).append(' ')
            val start = out.length
            out.append(value)
            if (differs) out.setSpan(ForegroundColorSpan(warn), start, out.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
            spoken += if (differs) "$label $value, differs from this survey" else "$label $value"
        }
        val c = current
        field("net", e.prefix, c != null && e.prefix != c.prefix)
        field("gw", e.gateway, c != null && c.gateway.isNotEmpty() && e.gateway != c.gateway)
        out.append('\n')
        field("dhcp", e.dhcpServer, c != null && c.dhcpServer.isNotEmpty() && e.dhcpServer != c.dhcpServer)
        field("dns", e.resolvers.joinToString(","), c != null && c.resolvers.isNotEmpty() && e.resolvers.toSet() != c.resolvers.toSet())
        out.append('\n').append(getString(R.string.compare_devices, e.devices))
        spoken += getString(R.string.compare_devices, e.devices)
        return out to spoken.joinToString(", ")
    }

    private fun formatTime(iso: String, format: DateFormat): String = runCatching {
        // Go writes the offset it ran under ("+05:30"), which Instant.parse
        // rejects on older Android; OffsetDateTime takes either form.
        format.format(Date.from(OffsetDateTime.parse(iso).toInstant()))
    }.getOrDefault(iso)

    private companion object {
        const val KEY_DRIFT_EVENT = "drift-event"

        // embedded: the native bar carries the brand, so the page drops its own.
        private fun index(vararg query: Pair<String, String>): String =
            Uri.Builder().scheme("https").authority(WebViewAssetLoader.DEFAULT_DOMAIN).path("/assets/index.html")
                .appendQueryParameter("embedded", "1")
                .apply { query.forEach { (k, v) -> appendQueryParameter(k, v) } }
                .build().toString()

        val INDEX: String = index()
        val INDEX_DRIFT: String = index("tab" to "drift")
    }
}
