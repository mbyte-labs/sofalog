package ro.mbyte.sofalog.data.tmdbaccount

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.AtomicFile
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import java.io.File
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** What the store holds. Everything in it is a secret or leads to one; none of it is ever logged. */
@Serializable
data class TmdbLinkSecrets(
    /** The linked account's session id. */
    val session: String? = null,
    /** A link attempt waiting for the user's approval on TMDB's page. */
    val pendingToken: String? = null,
    val pendingStartedAt: String? = null,
    /** A session already exchanged for the pending token, whose account has not been read yet. */
    val pendingSession: String? = null,
)

/**
 * The TMDB session id is a secret like the app's key, so it lives outside the database: one file
 * in the no-backup directory — which no device backup or transfer ever includes — encrypted with
 * a key that never leaves this device's keystore. A file that cannot be read (another device's,
 * a lost key) reads as "nothing held", which the app shows as a link to make again.
 */
class TmdbSessionStore(context: Context) {
    private val file = AtomicFile(File(context.noBackupFilesDir, "tmdb_link"))
    private val json = Json { ignoreUnknownKeys = true }

    // The last value written, so a keystore that fails to persist still serves this process.
    @Volatile private var cached: TmdbLinkSecrets? = null

    @Synchronized
    fun read(): TmdbLinkSecrets = cached ?: load().also { cached = it }

    @Synchronized
    fun write(secrets: TmdbLinkSecrets) {
        cached = secrets
        runCatching {
            if (secrets == TmdbLinkSecrets()) {
                file.delete()
                return
            }
            val cipher = Cipher.getInstance(TRANSFORMATION).apply { init(Cipher.ENCRYPT_MODE, key()) }
            val sealed = cipher.doFinal(json.encodeToString(TmdbLinkSecrets.serializer(), secrets).toByteArray())
            val out = file.startWrite()
            try {
                out.write(cipher.iv.size)
                out.write(cipher.iv)
                out.write(sealed)
                file.finishWrite(out)
            } catch (e: Exception) {
                file.failWrite(out)
            }
        }
    }

    fun update(change: (TmdbLinkSecrets) -> TmdbLinkSecrets) = write(change(read()))

    private fun load(): TmdbLinkSecrets = runCatching {
        val bytes = file.readFully()
        val ivSize = bytes[0].toInt()
        val cipher = Cipher.getInstance(TRANSFORMATION).apply {
            init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, bytes, 1, ivSize))
        }
        val plain = cipher.doFinal(bytes, 1 + ivSize, bytes.size - 1 - ivSize)
        json.decodeFromString(TmdbLinkSecrets.serializer(), String(plain))
    }.getOrDefault(TmdbLinkSecrets())

    private fun key(): SecretKey {
        val store = KeyStore.getInstance(KEYSTORE).apply { load(null) }
        (store.getKey(ALIAS, null) as? SecretKey)?.let { return it }
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KEYSTORE).apply {
            init(
                KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                    .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                    .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                    .build(),
            )
        }.generateKey()
    }

    private companion object {
        const val KEYSTORE = "AndroidKeyStore"
        const val ALIAS = "sofalog_tmdb_link"
        const val TRANSFORMATION = "AES/GCM/NoPadding"
    }
}
