package com.goroscope.plugin

import com.intellij.openapi.Disposable
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.Disposer
import com.intellij.ui.jcef.JBCefApp
import com.intellij.ui.jcef.JBCefBrowser
import com.intellij.ui.jcef.JBCefBrowserBase
import com.intellij.ui.jcef.JBCefJSQuery
import com.intellij.ui.components.JBLabel
import com.intellij.util.ui.JBUI
import org.cef.browser.CefBrowser
import org.cef.browser.CefFrame
import org.cef.handler.CefLoadHandlerAdapter
import java.awt.BorderLayout
import java.awt.Desktop
import java.net.URI
import javax.swing.*

/**
 * GoroscopeTimelinePanel embeds the Goroscope React UI inside the IDE using
 * JCEF (Chromium Embedded Framework).  When JCEF is unavailable (e.g. in
 * headless/remote mode), a fallback panel with a clickable link is shown.
 *
 * The panel intercepts `window.postMessage` events of type `goroscope:openFile`
 * from the Goroscope UI and uses a [JBCefJSQuery] to relay them back to the
 * IDE, which then opens the referenced file and navigates to the given line.
 */
class GoroscopeTimelinePanel(private val project: Project) : Disposable {

    val component: JComponent = buildPanel()

    private var browser: JBCefBrowser? = null
    private var openFileQuery: JBCefJSQuery? = null

    private fun buildPanel(): JComponent {
        if (!JBCefApp.isSupported()) {
            return buildFallbackPanel()
        }

        val jcefBrowser = JBCefBrowser.createBuilder()
            .setOffScreenRendering(false)
            .build()
        browser = jcefBrowser

        // Set up a JS→Kotlin bridge for the openFile message.
        val query = JBCefJSQuery.create(jcefBrowser as JBCefBrowserBase)
        openFileQuery = query
        query.addHandler { payload -> handleOpenFile(payload); null }

        Disposer.register(this, jcefBrowser)
        Disposer.register(this, query)

        // Inject the message bridge once the page finishes loading.
        jcefBrowser.jbCefClient.addLoadHandler(object : CefLoadHandlerAdapter() {
            override fun onLoadEnd(browser: CefBrowser, frame: CefFrame, httpStatusCode: Int) {
                if (!frame.isMain) return
                val js = """
                    (function() {
                      window.addEventListener('message', function(event) {
                        var d = event.data;
                        if (d && d.type === 'goroscope:openFile' && d.file) {
                          ${query.inject("""JSON.stringify({file: d.file, line: d.line || 1})""")}
                        }
                      });
                    })();
                """.trimIndent()
                browser.executeJavaScript(js, browser.url, 0)
            }
        }, jcefBrowser.cefBrowser)

        val panel = JPanel(BorderLayout())

        // Toolbar with an "Open in Browser" button.
        val toolbar = JPanel(BorderLayout())
        toolbar.border = JBUI.Borders.empty(2, 4)
        toolbar.add(JBLabel("Goroscope Timeline"), BorderLayout.WEST)
        val openBtn = JButton("Open in Browser").apply {
            addActionListener { openInBrowser() }
        }
        val reloadBtn = JButton("Reload").apply {
            addActionListener { loadTimeline() }
        }
        val btnPanel = JPanel()
        btnPanel.add(reloadBtn)
        btnPanel.add(openBtn)
        toolbar.add(btnPanel, BorderLayout.EAST)

        panel.add(toolbar, BorderLayout.NORTH)
        panel.add(jcefBrowser.component, BorderLayout.CENTER)

        loadTimeline()
        return panel
    }

    private fun buildFallbackPanel(): JPanel {
        val panel = JPanel(BorderLayout())
        panel.border = JBUI.Borders.empty(16)
        val url = GoroscopeService.instance.uiUrl()
        val label = JLabel("<html>JCEF is not available in this IDE installation.<br><br>" +
                "Open the Goroscope timeline in your browser:<br>" +
                "<a href='$url'>$url</a></html>").apply {
            addMouseListener(object : java.awt.event.MouseAdapter() {
                override fun mouseClicked(e: java.awt.event.MouseEvent) {
                    openInBrowser()
                }
            })
        }
        panel.add(label, BorderLayout.NORTH)
        return panel
    }

    private fun loadTimeline() {
        browser?.loadURL(GoroscopeService.instance.uiUrl())
    }

    private fun openInBrowser() {
        val url = GoroscopeService.instance.uiUrl()
        try {
            Desktop.getDesktop().browse(URI(url))
        } catch (e: Exception) {
            JOptionPane.showMessageDialog(null, "Cannot open browser: ${e.message}")
        }
    }

    /**
     * Handle a JSON payload `{"file":"/abs/path/main.go","line":42}` sent by
     * the Goroscope UI when the user clicks a stack frame.
     *
     * Opens the file in the IDE editor at the specified line.
     */
    private fun handleOpenFile(payload: String) {
        // Minimal JSON extraction — avoids pulling in a JSON library.
        val file = payload.substringAfter(""""file":"""").substringAfter('"').substringBefore('"')
        val lineStr = payload.substringAfter(""""line":""").trimStart().takeWhile { it.isDigit() }
        val line = lineStr.toIntOrNull()?.coerceAtLeast(1) ?: 1

        if (file.isBlank()) return

        SwingUtilities.invokeLater {
            com.intellij.openapi.application.ApplicationManager.getApplication().invokeLater {
                try {
                    val vFile = com.intellij.openapi.vfs.LocalFileSystem.getInstance()
                        .findFileByPath(file) ?: return@invokeLater
                    com.intellij.openapi.fileEditor.FileEditorManager.getInstance(project)
                        .openFile(vFile, true)
                    val editors = com.intellij.openapi.fileEditor.FileEditorManager
                        .getInstance(project).getEditors(vFile)
                    for (editor in editors) {
                        val textEditor = editor as? com.intellij.openapi.fileEditor.TextEditor ?: continue
                        val doc = textEditor.editor.document
                        val offset = doc.getLineStartOffset((line - 1).coerceIn(0, doc.lineCount - 1))
                        textEditor.editor.caretModel.moveToOffset(offset)
                        textEditor.editor.scrollingModel.scrollToCaret(
                            com.intellij.openapi.editor.ScrollType.CENTER
                        )
                        break
                    }
                } catch (e: Exception) {
                    // Navigation failure is non-fatal.
                }
            }
        }
    }

    override fun dispose() {
        // browser and query disposal is handled by Disposer.register above.
    }
}
