package com.goroscope.plugin

import com.intellij.openapi.options.Configurable
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBTextField
import com.intellij.util.ui.FormBuilder
import javax.swing.JComponent
import javax.swing.JPanel
import javax.swing.JSpinner
import javax.swing.SpinnerNumberModel

/**
 * GoroscopeConfigurable provides the Settings page under Tools → Goroscope.
 *
 * Fields:
 * - **Addr** — HTTP address where goroscope listens (default: 127.0.0.1:7070)
 * - **Binary path** — absolute path to the goroscope binary (empty = use PATH)
 * - **Poll interval** — session status panel refresh rate in milliseconds
 */
class GoroscopeConfigurable : Configurable {

    private val addrField = JBTextField()
    private val binaryPathField = JBTextField()
    private val pollIntervalSpinner = JSpinner(SpinnerNumberModel(2000, 500, 60_000, 500))

    private var panel: JPanel? = null

    override fun getDisplayName(): String = "Goroscope"

    override fun createComponent(): JComponent {
        val p = FormBuilder.createFormBuilder()
            .addLabeledComponent(JBLabel("HTTP address:"), addrField, 1, false)
            .addLabeledComponent(JBLabel("Binary path:"), binaryPathField, 1, false)
            .addLabeledComponent(JBLabel("Poll interval (ms):"), pollIntervalSpinner, 1, false)
            .addComponentFillVertically(JPanel(), 0)
            .panel
        panel = p
        return p
    }

    override fun isModified(): Boolean {
        val s = GoroscopeSettings.instance
        return addrField.text != s.addr ||
                binaryPathField.text != s.binaryPath ||
                (pollIntervalSpinner.value as Int).toLong() != s.pollIntervalMs
    }

    override fun apply() {
        val s = GoroscopeSettings.instance
        s.addr = addrField.text.trim().ifBlank { GoroscopeSettings.DEFAULT_ADDR }
        s.binaryPath = binaryPathField.text.trim()
        s.pollIntervalMs = (pollIntervalSpinner.value as Int).toLong()
    }

    override fun reset() {
        val s = GoroscopeSettings.instance
        addrField.text = s.addr
        binaryPathField.text = s.binaryPath
        pollIntervalSpinner.value = s.pollIntervalMs.toInt()
    }
}
