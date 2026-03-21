package com.goroscope.plugin

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.components.PersistentStateComponent
import com.intellij.openapi.components.Service
import com.intellij.openapi.components.State
import com.intellij.openapi.components.Storage

/**
 * GoroscopeSettings persists user configuration across IDE restarts.
 *
 * Settings are stored in `goroscope.xml` inside the IDE config directory.
 * Access via [GoroscopeSettings.instance].
 */
@Service(Service.Level.APP)
@State(name = "GoroscopeSettings", storages = [Storage("goroscope.xml")])
class GoroscopeSettings : PersistentStateComponent<GoroscopeSettings.State> {

    data class State(
        /** HTTP bind address where the goroscope server listens. */
        var addr: String = DEFAULT_ADDR,
        /**
         * Path to the goroscope binary.  Empty string means use PATH or the
         * workspace's `bin/goroscope`.
         */
        var binaryPath: String = "",
        /**
         * Poll interval in milliseconds for the session status panel.
         * Defaults to 2 000 ms.
         */
        var pollIntervalMs: Long = 2_000L,
    )

    private var state = State()

    override fun getState(): State = state

    override fun loadState(state: State) {
        this.state = state
    }

    var addr: String
        get() = state.addr
        set(value) { state.addr = value }

    var binaryPath: String
        get() = state.binaryPath
        set(value) { state.binaryPath = value }

    var pollIntervalMs: Long
        get() = state.pollIntervalMs
        set(value) { state.pollIntervalMs = value }

    companion object {
        const val DEFAULT_ADDR = "127.0.0.1:7070"

        val instance: GoroscopeSettings
            get() = ApplicationManager.getApplication().getService(GoroscopeSettings::class.java)
    }
}
