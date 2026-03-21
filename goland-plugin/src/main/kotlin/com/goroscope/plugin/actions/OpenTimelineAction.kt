package com.goroscope.plugin.actions

import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.wm.ToolWindowManager

/**
 * OpenTimelineAction shows the Goroscope tool window and selects the Timeline
 * tab.  This is useful when the user has already started a session externally
 * (e.g. via `go run` + agent) and just wants to view the UI.
 */
class OpenTimelineAction : AnAction() {

    override fun getActionUpdateThread(): ActionUpdateThread = ActionUpdateThread.BGT

    override fun update(e: AnActionEvent) {
        e.presentation.isEnabled = e.project != null
    }

    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        ApplicationManager.getApplication().invokeLater {
            val tw = ToolWindowManager.getInstance(project).getToolWindow("Goroscope")
            tw?.show {
                tw.contentManager.let { cm ->
                    if (cm.contentCount > 1) cm.setSelectedContent(cm.getContent(1)!!)
                }
            }
        }
    }
}
