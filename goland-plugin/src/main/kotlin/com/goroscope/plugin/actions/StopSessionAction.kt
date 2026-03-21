package com.goroscope.plugin.actions

import com.goroscope.plugin.GoroscopeService
import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.ui.Messages

/**
 * StopSessionAction stops the running goroscope subprocess (if any) and shows
 * a notification in the status bar.
 */
class StopSessionAction : AnAction() {

    override fun getActionUpdateThread(): ActionUpdateThread = ActionUpdateThread.BGT

    override fun update(e: AnActionEvent) {
        val running = GoroscopeService.instance.isRunning
        e.presentation.isEnabled = e.project != null && running
        e.presentation.text = if (running) "Stop Session" else "Stop Session (not running)"
    }

    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        val service = GoroscopeService.instance
        if (!service.isRunning) {
            Messages.showInfoMessage(project, "No active Goroscope session.", "Goroscope")
            return
        }
        service.stop()
        Messages.showInfoMessage(project, "Goroscope session stopped.", "Goroscope")
    }
}
