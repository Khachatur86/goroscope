package com.goroscope.plugin

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.components.Service
import com.intellij.openapi.diagnostic.logger
import com.intellij.openapi.project.Project
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.atomic.AtomicReference

private val LOG = logger<GoroscopeService>()

/**
 * GoroscopeService is an application-level service that manages the lifecycle
 * of the goroscope subprocess and provides helpers for communicating with it.
 *
 * Acquire via [GoroscopeService.instance].
 */
@Service(Service.Level.APP)
class GoroscopeService {

    private val processRef = AtomicReference<Process?>(null)

    // ── Process management ────────────────────────────────────────────────────

    /**
     * Start the goroscope binary with the given arguments in [workDir].
     * Any previously running process is stopped first.
     *
     * @param args  arguments to pass after the binary (e.g. ["run", "./..."]).
     * @param workDir  the working directory; usually the project root.
     * @return true if the process was successfully launched.
     */
    fun start(args: List<String>, workDir: File): Boolean {
        stop()

        val binaryPath = resolveBinary(workDir)
        if (binaryPath == null) {
            LOG.warn("goroscope binary not found; set binaryPath in settings or install via 'go install'")
            return false
        }

        val settings = GoroscopeSettings.instance
        val command = buildList {
            add(binaryPath)
            addAll(args)
            add("--addr")
            add(settings.addr)
        }

        LOG.info("starting goroscope: ${command.joinToString(" ")}")

        val process = ProcessBuilder(command)
            .directory(workDir)
            .redirectErrorStream(false)
            .start()

        processRef.set(process)

        // Drain stdout/stderr so the process doesn't block on full pipes.
        process.inputStream.bufferedReader().forEachLine { LOG.debug("[goroscope] $it") }
        process.errorStream.bufferedReader().forEachLine { LOG.info("[goroscope-err] $it") }

        return true
    }

    /** Stop any running goroscope process, waiting up to 3 seconds for a clean exit. */
    fun stop() {
        val process = processRef.getAndSet(null) ?: return
        LOG.info("stopping goroscope process (pid=${process.pid()})")
        process.destroy()
        if (!process.waitFor(3, java.util.concurrent.TimeUnit.SECONDS)) {
            LOG.warn("goroscope did not stop in 3 s — sending SIGKILL")
            process.destroyForcibly()
        }
    }

    /** Whether a goroscope subprocess is currently running. */
    val isRunning: Boolean
        get() = processRef.get()?.isAlive == true

    // ── API helpers ───────────────────────────────────────────────────────────

    /**
     * Fetch the current session from the goroscope API.
     * Returns null on any error (connection refused, parse failure, etc.).
     */
    fun fetchCurrentSession(): SessionInfo? {
        val addr = GoroscopeSettings.instance.addr
        return try {
            val url = URL("http://$addr/api/v1/session/current")
            val conn = url.openConnection() as HttpURLConnection
            conn.connectTimeout = 2_000
            conn.readTimeout = 2_000
            conn.requestMethod = "GET"
            if (conn.responseCode != 200) return null
            val body = conn.inputStream.bufferedReader().readText()
            parseSession(body)
        } catch (e: Exception) {
            LOG.debug("fetchCurrentSession error: ${e.message}")
            null
        }
    }

    /** UI URL for the currently configured goroscope address. */
    fun uiUrl(): String = "http://${GoroscopeSettings.instance.addr}"

    // ── Binary resolution ─────────────────────────────────────────────────────

    /**
     * Resolve the absolute path to the goroscope binary.
     * Resolution order:
     * 1. `GoroscopeSettings.binaryPath` if non-blank and the file exists.
     * 2. `<workDir>/bin/goroscope[.exe]`.
     * 3. `goroscope` on PATH (returned as-is; the OS resolves it).
     */
    fun resolveBinary(workDir: File): String? {
        val settings = GoroscopeSettings.instance
        if (settings.binaryPath.isNotBlank()) {
            val file = File(settings.binaryPath)
            if (file.exists()) return file.absolutePath
            LOG.warn("configured binaryPath '${settings.binaryPath}' does not exist")
            return null
        }

        val exe = if (System.getProperty("os.name").lowercase().startsWith("win")) ".exe" else ""
        val workspaceBin = File(workDir, "bin/goroscope$exe")
        if (workspaceBin.exists()) return workspaceBin.absolutePath

        // Fall back to PATH lookup.
        return "goroscope$exe"
    }

    // ── Simple JSON parsing (no external dependency) ──────────────────────────

    private fun parseSession(json: String): SessionInfo? {
        return try {
            SessionInfo(
                id = jsonString(json, "id") ?: return null,
                name = jsonString(json, "name") ?: "",
                target = jsonString(json, "target") ?: "",
                status = jsonString(json, "status") ?: "unknown",
                startedAt = jsonString(json, "started_at"),
                error = jsonString(json, "error"),
            )
        } catch (e: Exception) {
            null
        }
    }

    /** Extracts a string value for [key] from a flat JSON object (no nesting). */
    private fun jsonString(json: String, key: String): String? {
        val pattern = Regex(""""$key"\s*:\s*"([^"\\]*(?:\\.[^"\\]*)*)"""")
        return pattern.find(json)?.groupValues?.get(1)
    }

    companion object {
        val instance: GoroscopeService
            get() = ApplicationManager.getApplication().getService(GoroscopeService::class.java)
    }
}

/** Lightweight representation of the current session returned by the API. */
data class SessionInfo(
    val id: String,
    val name: String,
    val target: String,
    val status: String,
    val startedAt: String?,
    val error: String?,
)
