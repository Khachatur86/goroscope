package com.goroscope.plugin

import com.intellij.openapi.project.Project
import com.intellij.openapi.wm.ToolWindow
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBScrollPane
import com.intellij.util.ui.JBUI
import com.intellij.util.ui.UIUtil
import java.awt.BorderLayout
import java.awt.Font
import java.awt.GridBagConstraints
import java.awt.GridBagLayout
import java.awt.event.ActionEvent
import javax.swing.*

/**
 * GoroscopeSessionPanel shows the current session status inside the Goroscope
 * tool window.  It polls [GoroscopeService.fetchCurrentSession] at the
 * configured interval and updates the label grid.
 */
class GoroscopeSessionPanel(
    private val project: Project,
    private val toolWindow: ToolWindow,
) {
    val component: JComponent = buildPanel()

    private val statusLabel = JBLabel("—")
    private val nameLabel = JBLabel("—")
    private val targetLabel = JBLabel("—")
    private val startedLabel = JBLabel("—")
    private val errorLabel = JBLabel("—").apply {
        foreground = UIUtil.getErrorForeground()
    }
    private val connectionLabel = JBLabel("Waiting for goroscope…").apply {
        foreground = UIUtil.getLabelDisabledForeground()
    }

    private var pollTimer: Timer? = null

    private fun buildPanel(): JPanel {
        val root = JPanel(BorderLayout())
        root.border = JBUI.Borders.empty(8)

        // ── Connection status bar ─────────────────────────────────────────
        val topBar = JPanel(BorderLayout())
        topBar.add(connectionLabel, BorderLayout.CENTER)

        val refreshButton = JButton("Refresh").apply {
            addActionListener { _: ActionEvent -> refresh() }
        }
        topBar.add(refreshButton, BorderLayout.EAST)
        root.add(topBar, BorderLayout.NORTH)

        // ── Session detail grid ───────────────────────────────────────────
        val grid = JPanel(GridBagLayout())
        val c = GridBagConstraints().apply {
            anchor = GridBagConstraints.WEST
            insets = JBUI.insets(2, 4)
        }

        fun addRow(label: String, valueLabel: JLabel, row: Int) {
            c.gridx = 0; c.gridy = row; c.weightx = 0.0
            grid.add(JBLabel("$label:").apply { font = font.deriveFont(Font.BOLD) }, c)
            c.gridx = 1; c.weightx = 1.0
            grid.add(valueLabel, c)
        }

        addRow("Name", nameLabel, 0)
        addRow("Status", statusLabel, 1)
        addRow("Target", targetLabel, 2)
        addRow("Started", startedLabel, 3)
        addRow("Error", errorLabel, 4)

        // Push rows to the top.
        c.gridx = 0; c.gridy = 5; c.weighty = 1.0
        c.fill = GridBagConstraints.VERTICAL
        grid.add(JPanel(), c)

        root.add(JBScrollPane(grid), BorderLayout.CENTER)

        startPolling()
        refresh()
        return root
    }

    private fun startPolling() {
        stopPolling()
        val intervalMs = GoroscopeSettings.instance.pollIntervalMs.toInt().coerceAtLeast(500)
        pollTimer = Timer(intervalMs) { refresh() }.also { it.start() }
    }

    private fun stopPolling() {
        pollTimer?.stop()
        pollTimer = null
    }

    /** Fetch session info on a background thread and update labels on the EDT. */
    fun refresh() {
        SwingUtilities.invokeLater {
            connectionLabel.text = "Connecting…"
        }
        Thread {
            val session = GoroscopeService.instance.fetchCurrentSession()
            SwingUtilities.invokeLater {
                if (session == null) {
                    connectionLabel.text = "Cannot connect to goroscope at ${GoroscopeSettings.instance.addr}"
                    connectionLabel.foreground = UIUtil.getLabelDisabledForeground()
                    nameLabel.text = "—"
                    statusLabel.text = "—"
                    targetLabel.text = "—"
                    startedLabel.text = "—"
                    errorLabel.text = "—"
                } else {
                    connectionLabel.text = "Connected to ${GoroscopeSettings.instance.addr}"
                    connectionLabel.foreground = UIUtil.getLabelForeground()
                    nameLabel.text = session.name.ifBlank { "—" }
                    statusLabel.text = session.status
                    targetLabel.text = session.target.ifBlank { "—" }
                    startedLabel.text = session.startedAt ?: "—"
                    errorLabel.text = session.error ?: "—"
                }
            }
        }.also { it.isDaemon = true }.start()
    }
}
