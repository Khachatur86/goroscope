package com.goroscope.plugin.actions

import com.goroscope.plugin.GoroscopeService
import com.goroscope.plugin.GoroscopeSettings
import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.ui.InputValidator
import com.intellij.openapi.ui.Messages
import com.intellij.openapi.wm.ToolWindowManager
import java.io.File

/**
 * AttachToSessionAction runs `goroscope attach <url>` against a user-provided
 * pprof endpoint and opens the Goroscope tool window timeline tab.
 *
 * The user is prompted for the target URL (default: http://localhost:6060).
 */
class AttachToSessionAction : AnAction() {

    override fun getActionUpdateThread(): ActionUpdateThread = ActionUpdateThread.BGT

    override fun update(e: AnActionEvent) {
        e.presentation.isEnabled = e.project != null
    }

    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return

        val targetUrl = Messages.showInputDialog(
            project,
            "Enter the pprof base URL of the running Go process:",
            "Goroscope: Attach to Process",
            null,
            "http://localhost:6060",
            object : InputValidator {
                override fun checkInput(input: String) = input.startsWith("http")
                override fun canClose(input: String) = checkInput(input)
            }
        ) ?: return // user cancelled

        val workDir = File(project.basePath ?: ".")
        val service = GoroscopeService.instance
        val addr = GoroscopeSettings.instance.addr

        val started = service.start(
            listOf("attach", targetUrl, "--addr", addr, "--open-browser=false"),
            workDir
        )

        if (!started) {
            Messages.showErrorDialog(
                project,
                "Goroscope binary not found.\n\n" +
                        "Install with: go install github.com/Khachatur86/goroscope/cmd/goroscope@latest",
                "Goroscope: Binary Not Found"
            )
            return
        }

        Thread.sleep(800) // wait for initial pprof poll to complete
        openTimelineToolWindow(project)
    }

    private fun openTimelineToolWindow(project: com.intellij.openapi.project.Project) {
        com.intellij.openapi.application.ApplicationManager.getApplication().invokeLater {
            val tw = ToolWindowManager.getInstance(project).getToolWindow("Goroscope")
            tw?.show {
                tw.contentManager.let { cm ->
                    if (cm.contentCount > 1) cm.setSelectedContent(cm.getContent(1)!!)
                }
            }
        }
    }
}
