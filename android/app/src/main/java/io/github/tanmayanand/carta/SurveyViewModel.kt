package io.github.tanmayanand.carta

import android.app.Application
import android.net.wifi.WifiManager
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import io.github.tanmayanand.carta.core.Core
import io.github.tanmayanand.carta.core.Progress
import io.github.tanmayanand.carta.core.Session
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import org.json.JSONObject
import kotlin.concurrent.thread
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/**
 * Owns the survey, so that it outlives the Activity: rotating the phone
 * mid-survey destroys the Activity, and a survey scoped to it would be killed
 * with it while its sockets kept probing.
 */
class SurveyViewModel(app: Application) : AndroidViewModel(app) {

    /** What the survey is doing. One state at a time; each has its own place on screen. */
    sealed interface Status {
        data object Idle : Status
        data class Running(val stage: String, val done: Long, val total: Long) : Status
        /** Stop was tapped and the Go side is winding down; its progress is no longer news. */
        data object Stopping : Status
        data class Done(val entry: SurveyStore.Entry) : Status
        data class Refused(val kind: OrientationProvider.Refusal, val reason: String) : Status
        /** [message] is the full sentence shown to the operator. */
        data class Failed(val message: String) : Status
    }

    /** A comparison the operator asked for; [event] changes once per new result. */
    data class Drift(val summary: String, val event: Int)

    data class UiState(
        val status: Status = Status.Idle,
        /** The survey on screen, if any. Kept apart from status so a refusal does not lose it. */
        val shown: SurveyStore.Entry? = null,
        /** Bumped whenever what the WebView should show changes, so it reloads. */
        val generation: Int = 0,
        val drift: Drift? = null,
    )

    private val store = SurveyStore(app)
    private val orientation = OrientationProvider(app)

    private val _state = MutableStateFlow(UiState())
    val state: StateFlow<UiState> = _state

    // Read by the WebView's request handler on a background thread.
    @Volatile var snapshot: String? = null
        private set
    @Volatile var drift: String? = null
        private set

    private var job: Job? = null
    @Volatile private var session: Session? = null

    init {
        viewModelScope.launch(Dispatchers.IO) {
            store.latest()?.let { (entry, json) ->
                snapshot = json
                _state.update { it.copy(status = Status.Done(entry), shown = entry, generation = it.generation + 1) }
            }
        }
    }

    val running: Boolean get() = job?.isActive == true

    fun survey() {
        if (running) return
        job = viewModelScope.launch {
            val profile = withContext(Dispatchers.IO) { orientation.read() }
            if (profile is OrientationProvider.Result.Refused) {
                _state.update { it.copy(status = Status.Refused(profile.kind, profile.reason)) }
                return@launch
            }
            profile as OrientationProvider.Result.Ready
            _state.update { it.copy(status = Status.Running("orient", 0, 0)) }
            val multicast = multicastLock()
            try {
                val s = withContext(Dispatchers.IO) { Core.newSession(profile.profileJson, OPTIONS) }
                session = s
                val json = s.runCancellable { stage, done, total ->
                    // A Go thread: only the StateFlow is touched here, never a View.
                    _state.update {
                        if (it.status is Status.Stopping) it
                        else it.copy(status = Status.Running(stage, done, total))
                    }
                }
                val entry = withContext(Dispatchers.IO) { store.save(json) }
                snapshot = json
                drift = null
                _state.update {
                    it.copy(status = Status.Done(entry), shown = entry, drift = null, generation = it.generation + 1)
                }
            } catch (e: kotlinx.coroutines.CancellationException) {
                throw e
            } catch (e: Exception) {
                val why = e.message ?: e.javaClass.simpleName
                _state.update { it.copy(status = Status.Failed(getApplication<Application>().getString(R.string.status_failed, why))) }
            } finally {
                session = null
                multicast?.release()
            }
        }
    }

    /**
     * Stops the survey and keeps what it found, as the CLI does on Ctrl-C: the
     * Go side returns a partial snapshot whose coverage says where it stopped.
     */
    fun stop() {
        val s = session ?: return
        _state.update { it.copy(status = Status.Stopping) }
        s.cancel()
    }

    /** Leaves a refusal for the survey that was on screen before it. */
    fun dismissRefusal() {
        _state.update { st -> st.copy(status = st.shown?.let { Status.Done(it) } ?: Status.Idle) }
    }

    /** The survey on screen, and every other survey, newest first, for the baseline sheet. */
    suspend fun baselines(): Pair<SurveyStore.Entry?, List<SurveyStore.Entry>> = withContext(Dispatchers.IO) {
        val current = _state.value.shown
        current to store.list().filter { it.id != current?.id }
    }

    fun compareWith(baseline: SurveyStore.Entry) {
        val current = snapshot ?: return
        viewModelScope.launch {
            try {
                val report = withContext(Dispatchers.IO) {
                    val before = store.read(baseline.id) ?: error("survey ${baseline.id} is missing")
                    Core.diff(before, current)
                }
                drift = report
                val summary = JSONObject(report).optString("summary")
                _state.update {
                    it.copy(drift = Drift(summary, (it.drift?.event ?: 0) + 1), generation = it.generation + 1)
                }
            } catch (e: Exception) {
                _state.update { it.copy(status = Status.Failed(getApplication<Application>().getString(R.string.compare_failed, e.message))) }
            }
        }
    }

    fun clearComparison() {
        drift = null
        _state.update { it.copy(drift = null, generation = it.generation + 1) }
    }

    override fun onCleared() {
        // The coroutine is cancelled with the scope; this makes sure the Go
        // side stops too, rather than probing on with nobody to report to.
        session?.cancel()
    }

    // Wi-Fi drivers drop multicast to save power; without the lock, mDNS and
    // SSDP replies addressed to the group are lost.
    private fun multicastLock(): WifiManager.MulticastLock? =
        getApplication<Application>().getSystemService(WifiManager::class.java)
            ?.createMulticastLock("carta-survey")
            ?.apply { setReferenceCounted(false); acquire() }

    private companion object {
        // The deadline is enforced in Go: a blocking JNI call cannot be
        // interrupted from Kotlin, so withTimeout here would stop nothing.
        const val OPTIONS = """{"deadline_seconds": 240, "timeout_ms": 700, "concurrency": 64}"""
    }
}

/**
 * Runs the blocking Go call on its own thread and ties coroutine cancellation
 * to [Session.cancel]. Cancelling the coroutine alone would abandon the call,
 * not stop it.
 */
private suspend fun Session.runCancellable(onStage: (String, Long, Long) -> Unit): String =
    suspendCancellableCoroutine { cont ->
        cont.invokeOnCancellation { cancel() }
        thread(name = "carta-survey") {
            try {
                cont.resume(run(Progress { stage, done, total -> onStage(stage, done, total) }))
            } catch (e: Exception) {
                cont.resumeWithException(e)
            }
        }
    }
