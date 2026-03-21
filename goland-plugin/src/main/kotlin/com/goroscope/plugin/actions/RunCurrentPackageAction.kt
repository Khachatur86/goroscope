package com.goroscope.plugin.actions

import com.goroscope.plugin.GoroscopeService
import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.CommonDataKeys
import com.intellij.openapi.wm.ToolWindowManager
import java.io.File

/**
 * RunCurrentPackageAction starts `goroscope run <package>` for the package
 * containing the currently active file, then opens the Goroscope tool window.
 */
class RunCurrentPackageAction : AnAction() {

    override fun getActionUpdateThread(): ActionUpdateThread = ActionUpdateThread.BGT

    override fun update(e: AnActionEvent) {
        e.presentation.isEnabled = e.project != null
    }

    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        val editor = e.getData(CommonDataKeys.EDITOR)
        val file = e.getData(CommonDataKeys.VIRTUAL_FILE)

        val workDir = File(project.basePath ?: ".")
        val target = if (file != null) {
            val dir = File(file.path).parentFile
            "./" + dir.relativeTo(workDir).path.replace('\\', '/')
        } else {
            "."
        }

        val service = GoroscopeService.instance
        val started = service.start(listOf("run", target), workDir)

        if (!started) {
            com.intellij.openapi.ui.Messages.showErrorDialog(
                project,
                "Goroscope binary not found.\n\n" +
                        "Install with: go install github.com/Khachatur86/goroscope/cmd/goroscope@latest\n" +
                        "Or set the binary path in Settings → Tools → Goroscope.",
                "Goroscope: Binary Not Found"
            )
            return
        }

        // Give the server a moment to start, then show the timeline tab.
        Thread.sleep(500)
        openTimelineToolWindow(project)
    }

    private fun openTimelineToolWindow(project: com.intellij.openapi.project.Project) {
        com.intellij.openapi.application.ApplicationManager.getApplication().invokeLater {
            val tw = ToolWindowManager.getInstance(project).getToolWindow("Goroscope")
            tw?.show {
                // Select the Timeline tab (index 1).
                tw.contentManager.let { cm ->
                    if (cm.contentCount > 1) cm.setSelectedContent(cm.getContent(1)!!)
                }
            }
        }
    }
}
